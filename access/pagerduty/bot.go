package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"text/template"
	"time"

	pd "github.com/PagerDuty/go-pagerduty"

	"github.com/gravitational/trace"
	// log "github.com/sirupsen/logrus"
)

const (
	pdMaxConns    = 100
	pdHTTPTimeout = 10 * time.Second
	pdListLimit   = uint(60)

	pdIncidentKeyPrefix  = "teleport-access-request"
	pdApproveAction      = "approve"
	pdApproveActionLabel = "Approve Request"
	pdDenyAction         = "deny"
	pdDenyActionLabel    = "Deny Request"
)

var incidentBodyTemplate *template.Template

func init() {
	var err error
	incidentBodyTemplate, err = template.New("description").Parse(
		`{{.User}} requested permissions for roles {{range $index, $element := .Roles}}{{if $index}}, {{end}}{{ . }}{{end}} on Teleport at {{.Created.Format .TimeFormat}}. To approve or deny the request, please use Special Actions on this incident.
`,
	)
	if err != nil {
		panic(err)
	}
}

// Bot is a wrapper around pd.Client that works with access.Request
type Bot struct {
	httpClient  *http.Client
	apiEndpoint string
	apiKey      string
	server      *WebhookServer
	from        string
	serviceID   string

	clusterName string
}

type HTTPClientImpl func(*http.Request) (*http.Response, error)

func (h HTTPClientImpl) Do(req *http.Request) (*http.Response, error) {
	return h(req)
}

func NewBot(conf PagerdutyConfig, server *WebhookServer) *Bot {
	httpClient := &http.Client{
		Timeout: pdHTTPTimeout,
		Transport: &http.Transport{
			MaxConnsPerHost:     pdMaxConns,
			MaxIdleConnsPerHost: pdMaxConns,
		},
	}
	return &Bot{
		httpClient:  httpClient,
		server:      server,
		apiEndpoint: conf.APIEndpoint,
		apiKey:      conf.APIKey,
		from:        conf.UserEmail,
		serviceID:   conf.ServiceID,
	}
}

func (b *Bot) NewClient(ctx context.Context) *pd.Client {
	clientOpts := []pd.ClientOptions{}
	// apiEndpoint parameter is set only in tests
	if b.apiEndpoint != "" {
		clientOpts = append(clientOpts, pd.WithAPIEndpoint(b.apiEndpoint))
	}
	client := pd.NewClient(b.apiKey, clientOpts...)
	client.HTTPClient = HTTPClientImpl(func(r *http.Request) (*http.Response, error) {
		return b.httpClient.Do(r.WithContext(ctx))
	})
	return client
}

func (b *Bot) HealthCheck(ctx context.Context) error {
	client := b.NewClient(ctx)

	if _, err := client.GetService(b.serviceID, nil); err != nil {
		return trace.Wrap(err, "failed to fetch pagerduty service info: %v", err)
	}

	return nil
}

func (b *Bot) Setup(ctx context.Context) error {
	client := b.NewClient(ctx)

	var more bool
	var offset uint

	var webhookSchemaID string
	for offset, more = 0, true; webhookSchemaID == "" && more; {
		schemaResp, err := client.ListExtensionSchemas(pd.ListExtensionSchemaOptions{
			APIListObject: pd.APIListObject{
				Offset: offset,
				Limit:  pdListLimit,
			},
		})
		if err != nil {
			return trace.Wrap(err)
		}

		for _, schema := range schemaResp.ExtensionSchemas {
			if schema.Key == "custom_webhook" {
				webhookSchemaID = schema.ID
			}
		}

		more = schemaResp.More
		offset += pdListLimit
	}
	if webhookSchemaID == "" {
		return trace.NotFound(`failed to find "Custom Incident Action" extension type`)
	}

	var approveExtID, denyExtID string
	for offset, more = 0, true; (approveExtID == "" || denyExtID == "") && more; {
		extResp, err := client.ListExtensions(pd.ListExtensionOptions{
			APIListObject: pd.APIListObject{
				Offset: offset,
				Limit:  pdListLimit,
			},
			ExtensionObjectID: b.serviceID,
			ExtensionSchemaID: webhookSchemaID,
		})
		if err != nil {
			return trace.Wrap(err)
		}

		for _, ext := range extResp.Extensions {
			if ext.Name == pdApproveActionLabel {
				approveExtID = ext.ID
			}
			if ext.Name == pdDenyActionLabel {
				denyExtID = ext.ID
			}
		}

		more = extResp.More
		offset += pdListLimit
	}

	if err := b.setupCustomAction(client, approveExtID, webhookSchemaID, pdApproveAction, pdApproveActionLabel); err != nil {
		return err
	}
	if err := b.setupCustomAction(client, denyExtID, webhookSchemaID, pdDenyAction, pdDenyActionLabel); err != nil {
		return err
	}

	return nil
}

func (b *Bot) setupCustomAction(client *pd.Client, extensionID, schemaID, actionName, actionLabel string) error {
	actionURL := b.server.ActionURL(actionName)
	ext := &pd.Extension{
		Name:        actionLabel,
		EndpointURL: actionURL,
		ExtensionSchema: pd.APIObject{
			Type: "extension_schema_reference",
			ID:   schemaID,
		},
		ExtensionObjects: []pd.APIObject{
			pd.APIObject{
				Type: "service_reference",
				ID:   b.serviceID,
			},
		},
	}
	if extensionID == "" {
		_, err := client.CreateExtension(ext)
		return trace.Wrap(err)
	}
	_, err := client.UpdateExtension(extensionID, ext)
	return trace.Wrap(err)
}

func (b *Bot) CreateIncident(ctx context.Context, reqID string, reqData RequestData) (PagerdutyData, error) {
	client := b.NewClient(ctx)

	body, err := b.buildIncidentBody(reqID, reqData)
	if err != nil {
		return PagerdutyData{}, trace.Wrap(err)
	}

	incident, err := client.CreateIncident(b.from, &pd.CreateIncidentOptions{
		Title:       fmt.Sprintf("Access request from %s", reqData.User),
		IncidentKey: fmt.Sprintf("%s/%s", pdIncidentKeyPrefix, reqID),
		Service: &pd.APIReference{
			Type: "service_reference",
			ID:   b.serviceID,
		},
		Body: &pd.APIDetails{
			Type:    "incident_body",
			Details: body,
		},
	})
	if err != nil {
		return PagerdutyData{}, trace.Wrap(err)
	}

	return PagerdutyData{
		ID: incident.Id, // Yes, due to strange implementation, it's called `Id` which overrides `APIObject.ID`.
	}, nil
}

func (b *Bot) ResolveIncident(ctx context.Context, reqID string, pdData PagerdutyData, status string) error {
	client := b.NewClient(ctx)

	err := client.CreateIncidentNote(pdData.ID, pd.IncidentNote{
		User: pd.APIObject{
			Summary: b.from,
		},
		Content: fmt.Sprintf("Access request has been %s", status),
	})
	if err != nil {
		return trace.Wrap(err)
	}
	_, err = client.ManageIncidents(b.from, []pd.ManageIncidentsOptions{
		pd.ManageIncidentsOptions{
			ID:     pdData.ID,
			Type:   "incident_reference",
			Status: "resolved",
		},
	})
	return trace.Wrap(err)
}

func (b *Bot) GetUserInfo(ctx context.Context, userID string) (*pd.User, error) {
	client := b.NewClient(ctx)

	user, err := client.GetUser(userID, pd.GetUserOptions{})
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return user, nil
}

func (b *Bot) buildIncidentBody(reqID string, reqData RequestData) (string, error) {
	var builder strings.Builder
	err := incidentBodyTemplate.Execute(&builder, struct {
		ID         string
		TimeFormat string
		RequestData
	}{
		reqID,
		time.RFC822,
		reqData,
	})
	if err != nil {
		return "", trace.Wrap(err)
	}
	return builder.String(), nil
}
