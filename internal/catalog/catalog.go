package catalog

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
	"tailscale.com/client/tailscale/v2"
)

const DefaultEndpoint = "tcp:443"
const ServeVersion = "0.0.1"

var nameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// File is the on-disk catalog.
type File struct {
	Endpoint string    `yaml:"endpoint"`
	Access   Access    `yaml:"access"`
	Services []Service `yaml:"services"`
}

type Access struct {
	Src             []string `yaml:"src"`
	AutoApproveHost string   `yaml:"autoApproveHost"`
}

type Service struct {
	Name        string `yaml:"name"`
	DisplayName string `yaml:"displayName"`
	Local       string `yaml:"local"`
	Comment     string `yaml:"comment"`
}

func (s Service) SVCName() string {
	return "svc:" + s.Name
}

// CanonicalName strips a leading "svc:" so CLI args and API names match.
func CanonicalName(name string) string {
	return strings.TrimPrefix(strings.TrimSpace(name), "svc:")
}

// Lookup returns the catalog service matching name or svc:name.
func (f *File) Lookup(name string) (Service, bool) {
	want := CanonicalName(name)
	for _, s := range f.Services {
		if s.Name == want {
			return s, true
		}
	}
	return Service{}, false
}

func Load(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f File
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if f.Endpoint == "" {
		f.Endpoint = DefaultEndpoint
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

func (f *File) Validate() error {
	if NormalizePort(f.Endpoint) == "" {
		return fmt.Errorf("endpoint is required")
	}
	f.Endpoint = NormalizePort(f.Endpoint)
	if len(f.Services) == 0 {
		return fmt.Errorf("catalog has no services")
	}
	seen := make(map[string]struct{}, len(f.Services))
	for i := range f.Services {
		s := &f.Services[i]
		if !nameRE.MatchString(s.Name) {
			return fmt.Errorf("service %q: name must be a DNS label (lowercase, digits, hyphen)", s.Name)
		}
		if _, ok := seen[s.Name]; ok {
			return fmt.Errorf("duplicate service %q", s.Name)
		}
		seen[s.Name] = struct{}{}
		if err := validateLocal(s.Local); err != nil {
			return fmt.Errorf("service %q: %w", s.Name, err)
		}
	}
	return nil
}

func validateLocal(local string) error {
	u, err := url.Parse(local)
	if err != nil {
		return fmt.Errorf("local %q: %w", local, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("local %q: scheme must be http or https", local)
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		return fmt.Errorf("local %q: host:port required", local)
	}
	if port == "" {
		return fmt.Errorf("local %q: port required", local)
	}
	switch host {
	case "127.0.0.1", "localhost", "::1":
	default:
		return fmt.Errorf("local %q: must bind loopback (127.0.0.1), not %s", local, host)
	}
	return nil
}

// NormalizePort accepts "443" or "tcp:443" and returns "tcp:443".
func NormalizePort(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if !strings.Contains(p, ":") {
		return "tcp:" + p
	}
	return p
}

func PortsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	count := map[string]int{}
	for _, p := range a {
		count[NormalizePort(p)]++
	}
	for _, p := range b {
		n := NormalizePort(p)
		if count[n] == 0 {
			return false
		}
		count[n]--
	}
	return true
}

func (s Service) VIPService(endpoint string) tailscale.Service {
	return tailscale.Service{
		Name:    s.SVCName(),
		Comment: s.Comment,
		Ports:   []string{endpoint},
	}
}

func (f *File) VIPServices() []tailscale.Service {
	out := make([]tailscale.Service, 0, len(f.Services))
	for _, s := range f.Services {
		out = append(out, s.VIPService(f.Endpoint))
	}
	return out
}

// ServeFile is the JSON document for `tailscale serve set-config`.
type ServeFile struct {
	Version  string                  `json:"version"`
	Services map[string]ServeService `json:"services"`
}

type ServeService struct {
	Endpoints map[string]string `json:"endpoints"`
}

func (f *File) ServeConfig() *ServeFile {
	out := &ServeFile{
		Version:  ServeVersion,
		Services: make(map[string]ServeService, len(f.Services)),
	}
	for _, s := range f.Services {
		out.Services[s.SVCName()] = ServeService{
			Endpoints: map[string]string{f.Endpoint: s.Local},
		}
	}
	return out
}

func (f *File) Names() []string {
	names := make([]string, len(f.Services))
	for i, s := range f.Services {
		names[i] = s.SVCName()
	}
	return names
}
