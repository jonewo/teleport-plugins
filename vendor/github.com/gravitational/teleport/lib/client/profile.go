package client

import (
	"io/ioutil"
	"net"
	"os"
	"os/user"
	"path/filepath"

	"github.com/gravitational/trace"

	"gopkg.in/yaml.v2"
)

type ProfileOptions int

const (
	// ProfileCreateNew creates new profile, but does not update current profile
	ProfileCreateNew = 0
	// ProfileMakeCurrent creates a new profile and makes it current
	ProfileMakeCurrent = 1 << iota
)

// CurrentProfileSymlink is a filename which is a symlink to the
// current profile, usually something like this:
//
// ~/.tsh/profile -> ~/.tsh/staging.yaml
//
const CurrentProfileSymlink = "profile"

// ClientProfile is a collection of most frequently used CLI flags
// for "tsh".
//
// Profiles can be stored in a profile file, allowing TSH users to
// type fewer CLI args.
//
type ClientProfile struct {
	// WebProxyAddr is the host:port the web proxy can be accessed at.
	WebProxyAddr string `yaml:"web_proxy_addr,omitempty"`

	// SSHProxyAddr is the host:port the SSH proxy can be accessed at.
	SSHProxyAddr string `yaml:"ssh_proxy_addr,omitempty"`

	// KubeProxyAddr is the host:port the Kubernetes proxy can be accessed at.
	KubeProxyAddr string `yaml:"kube_proxy_addr,omitempty"`

	// Username is the Teleport username for the client.
	Username string `yaml:"user,omitempty"`

	// AuthType (like "google")
	AuthType string `yaml:"auth_type,omitempty"`

	// SiteName is equivalient to --cluster argument
	SiteName string `yaml:"cluster,omitempty"`

	// ForwardedPorts is the list of ports to forward to the target node.
	ForwardedPorts []string `yaml:"forward_ports,omitempty"`

	// DynamicForwardedPorts is a list of ports to use for dynamic port
	// forwarding (SOCKS5).
	DynamicForwardedPorts []string `yaml:"dynamic_forward_ports,omitempty"`
}

// Name returns the name of the profile.
func (c *ClientProfile) Name() string {
	addr, _, err := net.SplitHostPort(c.WebProxyAddr)
	if err != nil {
		return c.WebProxyAddr
	}

	return addr
}

// FullProfilePath returns the full path to the user profile directory.
// If the parameter is empty, it returns expanded "~/.tsh", otherwise
// returns its unmodified parameter
func FullProfilePath(pDir string) string {
	if pDir != "" {
		return pDir
	}
	// get user home dir:
	home := os.TempDir()
	u, err := user.Current()
	if err == nil {
		home = u.HomeDir
	}
	return filepath.Join(home, ProfileDir)

}

// ProfileFromDir reads the user (yaml) profile from a given directory. The
// default is to use the ~/<dir-path>/profile symlink unless another profile
// is explicitly asked for. It works by looking for a "profile" symlink in
// that directory pointing to the profile's YAML file first.
func ProfileFromDir(dirPath string, proxyName string) (*ClientProfile, error) {
	profilePath := filepath.Join(dirPath, CurrentProfileSymlink)
	if proxyName != "" {
		profilePath = filepath.Join(dirPath, proxyName+".yaml")
	}

	return ProfileFromFile(profilePath)
}

// ProfileFromFile loads the profile from a YAML file
func ProfileFromFile(filePath string) (*ClientProfile, error) {
	bytes, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	var cp *ClientProfile
	if err = yaml.Unmarshal(bytes, &cp); err != nil {
		return nil, trace.Wrap(err)
	}
	return cp, nil
}

// ProfileLocation specifies profile on -disk location
// and it's parameters on disk
type ProfileLocation struct {
	// AliasPath is an optional alias
	AliasPath string
	// Path is a profile file path on disk
	Path string
	// Options is a set of profile options
	Options ProfileOptions
}

// SaveTo saves the profile into a given filename, optionally overwriting it.
func (cp *ClientProfile) SaveTo(loc ProfileLocation) error {
	bytes, err := yaml.Marshal(&cp)
	if err != nil {
		return trace.Wrap(err)
	}
	if err = ioutil.WriteFile(loc.Path, bytes, 0660); err != nil {
		return trace.Wrap(err)
	}
	if loc.AliasPath != "" && filepath.Base(loc.AliasPath) != filepath.Base(loc.Path) {
		if err := os.Remove(loc.AliasPath); err != nil {
			log.Warningf("Failed to remove symlink alias: %v", err)
		}
		err := os.Symlink(filepath.Base(loc.Path), loc.AliasPath)
		if err != nil {
			log.Warningf("Failed to create profile alias: %v", err)
		}
	}
	// set 'current' symlink:
	if loc.Options&ProfileMakeCurrent != 0 {
		symlink := filepath.Join(filepath.Dir(loc.Path), CurrentProfileSymlink)
		if err := os.Remove(symlink); err != nil {
			log.Warningf("Failed to remove symlink: %v", err)
		}
		err = os.Symlink(filepath.Base(loc.Path), symlink)
	}
	return trace.Wrap(err)
}
