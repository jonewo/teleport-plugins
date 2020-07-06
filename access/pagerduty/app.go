package main

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/gravitational/teleport-plugins/access"
	"github.com/gravitational/teleport-plugins/utils"

	"github.com/gravitational/trace"

	log "github.com/sirupsen/logrus"
)

// App contains global application state.
type App struct {
	conf Config

	accessClient access.Client
	bot          *Bot
	webhookSrv   *WebhookServer
	mainJob      utils.ServiceJob

	*utils.Process
}

func NewApp(conf Config) (*App, error) {
	app := &App{conf: conf}
	app.mainJob = utils.NewServiceJob(app.run)
	return app, nil
}

// Run initializes and runs a watcher and a callback server
func (a *App) Run(ctx context.Context) error {
	// Initialize the process.
	a.Process = utils.NewProcess(ctx)
	a.SpawnCriticalJob(a.mainJob)
	<-a.Process.Done()
	return trace.Wrap(a.mainJob.Err())
}

// WaitReady waits for http and watcher service to start up.
func (a *App) WaitReady(ctx context.Context) (bool, error) {
	return a.mainJob.WaitReady(ctx)
}

func (a *App) PublicURL() *url.URL {
	if !a.mainJob.IsReady() {
		panic("app is not running")
	}
	return a.webhookSrv.BaseURL()
}

func (a *App) run(ctx context.Context) (err error) {
	log.Infof("Starting Teleport Access PagerDuty extension %s:%s", Version, Gitref)

	a.webhookSrv, err = NewWebhookServer(a.conf.HTTP, a.onPagerdutyAction)
	if err != nil {
		return
	}

	a.bot = NewBot(a.conf.Pagerduty, a.webhookSrv)

	tlsConf, err := access.LoadTLSConfig(
		a.conf.Teleport.ClientCrt,
		a.conf.Teleport.ClientKey,
		a.conf.Teleport.RootCAs,
	)
	if trace.Unwrap(err) == access.ErrInvalidCertificate {
		log.WithError(err).Warning("Auth client TLS configuration error")
	} else if err != nil {
		return
	}

	a.accessClient, err = access.NewClient(
		ctx,
		"pagerduty",
		a.conf.Teleport.AuthServer,
		tlsConf,
	)
	if err != nil {
		return
	}
	if err = a.checkTeleportVersion(ctx); err != nil {
		return
	}

	log.Debug("Starting PagerDuty API health check...")
	if err = a.bot.HealthCheck(ctx); err != nil {
		log.WithError(err).Error("PagerDuty API health check failed")
		return
	}
	log.Debug("PagerDuty API health check finished ok")

	err = a.webhookSrv.EnsureCert()
	if err != nil {
		return
	}
	httpJob := a.webhookSrv.ServiceJob()
	a.SpawnCriticalJob(httpJob)
	httpOk, err := httpJob.WaitReady(ctx)
	if err != nil {
		return
	}

	log.Debug("Setting up the webhook extensions")
	if err = a.bot.Setup(ctx); err != nil {
		log.WithError(err).Error("Failed to set up webhook extensions")
		return
	}
	log.Debug("PagerDuty webhook extensions setup finished ok")

	watcherJob := access.NewWatcherJob(
		a.accessClient,
		access.Filter{State: access.StatePending},
		a.onWatcherEvent,
	)
	a.SpawnCriticalJob(watcherJob)
	watcherOk, err := watcherJob.WaitReady(ctx)
	if err != nil {
		return
	}

	a.mainJob.SetReady(httpOk && watcherOk)

	<-httpJob.Done()
	<-watcherJob.Done()

	return trace.NewAggregate(httpJob.Err(), watcherJob.Err())
}

func (a *App) checkTeleportVersion(ctx context.Context) error {
	log.Debug("Checking Teleport server version")
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pong, err := a.accessClient.Ping(ctx)
	if err != nil {
		if trace.IsNotImplemented(err) {
			return trace.Wrap(err, "server version must be at least %s", access.MinServerVersion)
		}
		log.Error("Unable to get Teleport server version")
		return trace.Wrap(err)
	}
	a.bot.clusterName = pong.ClusterName
	err = pong.AssertServerVersion()
	return trace.Wrap(err)
}

func (a *App) onWatcherEvent(ctx context.Context, event access.Event) error {
	req, op := event.Request, event.Type
	switch op {
	case access.OpPut:
		if !req.State.IsPending() {
			log.WithField("event", event).Warn("non-pending request event")
			return nil
		}

		if err := a.onPendingRequest(ctx, req); err != nil {
			log := log.WithField("request_id", req.ID).WithError(err)
			log.Errorf("Failed to process pending request")
			log.Debugf("%v", trace.DebugReport(err))
			return err
		}
		return nil
	case access.OpDelete:
		if err := a.onDeletedRequest(ctx, req); err != nil {
			log := log.WithField("request_id", req.ID).WithError(err)
			log.Errorf("Failed to process deleted request")
			log.Debugf("%v", trace.DebugReport(err))
			return err
		}
		return nil
	default:
		return trace.BadParameter("unexpected event operation %s", op)
	}
}

func (a *App) onPagerdutyAction(ctx context.Context, action WebhookAction) error {
	log := log.WithFields(logFields{
		"pd_http_id": action.HTTPRequestID,
		"pd_msg_id":  action.MessageID,
	})

	if action.Event != "incident.custom" {
		log.Debugf("Got %q event, ignoring", action.Event)
		return nil
	}

	keyParts := strings.Split(action.IncidentKey, "/")
	if len(keyParts) != 2 || keyParts[0] != pdIncidentKeyPrefix {
		log.Debugf("Got unsupported incident key %q, ignoring", action.IncidentKey)
		return nil
	}

	reqID := keyParts[1]
	req, err := a.accessClient.GetRequest(ctx, reqID)

	if err != nil {
		if trace.IsNotFound(err) {
			log.WithError(err).WithField("request_id", reqID).Warning("Cannot process expired request")
			return nil
		}
		return trace.Wrap(err)
	}
	if req.State != access.StatePending {
		return trace.Errorf("cannot process not pending request: %+v", req)
	}

	pluginData, err := a.getPluginData(ctx, reqID)
	if err != nil {
		return trace.Wrap(err)
	}

	log = log.WithField("pd_incident_id", action.IncidentID)

	if pluginData.PagerdutyData.ID != action.IncidentID {
		log.WithField("plugin_data_incident_id", pluginData.PagerdutyData.ID).Debug("plugin_data.incident_id does not match incident.id")
		return trace.Errorf("incident_id from request's plugin_data does not match")
	}

	var (
		reqState   access.State
		resolution string
	)

	switch action.Name {
	case pdApproveAction:
		reqState = access.StateApproved
		resolution = "approved"
	case pdDenyAction:
		reqState = access.StateDenied
		resolution = "denied"
	default:
		return trace.BadParameter("unknown action: %q", action.Name)
	}

	if err := a.accessClient.SetRequestState(ctx, req.ID, reqState); err != nil {
		return trace.Wrap(err)
	}
	log.Infof("PagerDuty user %s the request", resolution)

	if err := a.bot.ResolveIncident(ctx, reqID, pluginData.PagerdutyData, resolution); err != nil {
		return trace.Wrap(err)
	}
	log.Infof("Incident %q has been resolved", action.IncidentID)

	return nil
}

func (a *App) onPendingRequest(ctx context.Context, req access.Request) error {
	reqData := RequestData{User: req.User, Roles: req.Roles, Created: req.Created}

	pdData, err := a.bot.CreateIncident(ctx, req.ID, reqData)
	if err != nil {
		return trace.Wrap(err)
	}

	log.WithFields(logFields{
		"request_id":     req.ID,
		"pd_incident_id": pdData.ID,
	}).Info("PagerDuty incident created")

	err = a.setPluginData(ctx, req.ID, PluginData{reqData, pdData})

	return trace.Wrap(err)
}

func (a *App) onDeletedRequest(ctx context.Context, req access.Request) error {
	reqID := req.ID // This is the only available field
	pluginData, err := a.getPluginData(ctx, reqID)
	if err != nil {
		if trace.IsNotFound(err) {
			log.WithError(err).Warn("Cannot expire unknown request")
			return nil
		}
		return trace.Wrap(err)
	}

	if err := a.bot.ResolveIncident(ctx, reqID, pluginData.PagerdutyData, "expired"); err != nil {
		return trace.Wrap(err)
	}

	log.WithField("request_id", reqID).Info("Successfully marked request as expired")

	return nil
}

func (a *App) getPluginData(ctx context.Context, reqID string) (PluginData, error) {
	dataMap, err := a.accessClient.GetPluginData(ctx, reqID)
	if err != nil {
		return PluginData{}, trace.Wrap(err)
	}
	return DecodePluginData(dataMap), nil
}

func (a *App) setPluginData(ctx context.Context, reqID string, data PluginData) error {
	return a.accessClient.UpdatePluginData(ctx, reqID, EncodePluginData(data), nil)
}
