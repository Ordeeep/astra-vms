// Package camera resolves vendor configuration into private RTSP origins.
// Media consumers do not depend on a camera manufacturer's HTTP protocol.
package camera

import (
	"context"
	"errors"
	"math"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type Profile struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	URL     string  `json:"url,omitempty"`
	Codec   string  `json:"codec,omitempty"`
	Width   int     `json:"width,omitempty"`
	Height  int     `json:"height,omitempty"`
	FPS     float64 `json:"fps,omitempty"`
	Enabled *bool   `json:"enabled,omitempty"`
}
type Connection struct {
	Host          string  `json:"host"`
	Protocol      string  `json:"protocol"`
	HTTPPort      int     `json:"httpPort"`
	RTSPPort      int     `json:"rtspPort,omitempty"`
	Username      string  `json:"username"`
	Password      *string `json:"password,omitempty"`
	HasPassword   bool    `json:"hasPassword,omitempty"`
	ProfileStatus string  `json:"profileStatus,omitempty"`
}
type Source struct {
	Type       string      `json:"connectionType"`
	Connection *Connection `json:"connection,omitempty"`
	Profiles   []Profile   `json:"profiles"`
}
type Device struct {
	Model    string `json:"model,omitempty"`
	Serial   string `json:"serial,omitempty"`
	Vendor   string `json:"vendor,omitempty"`
	Firmware string `json:"firmware,omitempty"`
}
type Discovery struct {
	Device   Device    `json:"device"`
	Profiles []Profile `json:"profiles"`
	RTSPPort int       `json:"-"`
}
type Adapter interface {
	ManagesProfiles() bool
	Validate(Source) error
	Resolve(Source, string) (string, error)
	Discover(context.Context, Source) (Discovery, error)
}

var registry = map[string]Adapter{"rtsp": rtsp{}, "dahua": dahua{}, "hikvision": hikvision{}}
var idPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
var managedProfilePattern = regexp.MustCompile(`^(main|sub[1-9][0-9]?)$`)

// ManagedProfiles distinguishes vendor discovery from manually configured RTSP.
// Adding another registered vendor does not require branches in inventory flows.
func ManagedProfiles(kind string) bool {
	adapter, ok := registry[kind]
	return ok && adapter.ManagesProfiles()
}

func Code(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func Normalize(s Source) (Source, error) {
	if s.Type == "" {
		s.Type = "rtsp"
	}
	if s.Connection != nil {
		c := *s.Connection
		c.Host = strings.TrimSpace(c.Host)
		c.HasPassword = false
		if c.Protocol == "" {
			c.Protocol = "http"
		}
		if c.HTTPPort == 0 {
			if c.Protocol == "https" {
				c.HTTPPort = 443
			} else {
				c.HTTPPort = 80
			}
		}
		s.Connection = &c
	}
	a, ok := registry[s.Type]
	if !ok {
		return s, errors.New("unsupported_connection_type")
	}
	if (len(s.Profiles) == 0 && !ManagedProfiles(s.Type)) || len(s.Profiles) > 16 {
		return s, errors.New("invalid_profiles")
	}
	seen := map[string]bool{}
	for _, p := range s.Profiles {
		if !idPattern.MatchString(p.ID) || seen[p.ID] || strings.TrimSpace(p.Name) == "" || len(p.Name) > 200 || len(p.Codec) > 32 || p.Width < 0 || p.Width > 32768 || p.Height < 0 || p.Height > 32768 || p.FPS < 0 || p.FPS > 1000 || math.IsNaN(p.FPS) || math.IsInf(p.FPS, 0) {
			return s, errors.New("invalid_profiles")
		}
		seen[p.ID] = true
	}
	if !seen["main"] && (!ManagedProfiles(s.Type) || len(s.Profiles) > 0) {
		return s, errors.New("invalid_profiles")
	}
	return s, a.Validate(s)
}
func Public(s Source) Source {
	p := s
	p.Profiles = append([]Profile{}, s.Profiles...)
	for i := range p.Profiles {
		p.Profiles[i].URL = ""
	}
	if s.Connection != nil {
		c := *s.Connection
		c.HasPassword = c.Password != nil && *c.Password != ""
		c.Password = nil
		if ManagedProfiles(s.Type) {
			c.RTSPPort = 0
		}
		p.Connection = &c
	}
	return p
}
func Resolve(s Source, id string) (string, error) {
	a, ok := registry[s.Type]
	if !ok {
		return "", errors.New("unsupported_connection_type")
	}
	return a.Resolve(s, id)
}
func Discover(ctx context.Context, s Source) (Discovery, error) {
	a, ok := registry[s.Type]
	if !ok {
		return Discovery{}, errors.New("unsupported_connection_type")
	}
	return a.Discover(ctx, s)
}
func DecodeLegacy(profiles []Profile) Source { return Source{Type: "rtsp", Profiles: profiles} }

type rtsp struct{}

func (rtsp) ManagesProfiles() bool { return false }

func (rtsp) Validate(s Source) error {
	if s.Connection != nil {
		return errors.New("invalid_connection")
	}
	for _, p := range s.Profiles {
		u, e := url.Parse(p.URL)
		if e != nil || len(p.URL) > 4096 || (u.Scheme != "rtsp" && u.Scheme != "rtsps") || u.Hostname() == "" || u.Fragment != "" {
			return errors.New("invalid_rtsp_url")
		}
	}
	return nil
}
func (rtsp) Resolve(s Source, id string) (string, error) {
	for _, p := range s.Profiles {
		if p.ID == id {
			return p.URL, nil
		}
	}
	return "", errors.New("profile_unavailable")
}
func (rtsp) Discover(context.Context, Source) (Discovery, error) {
	return Discovery{}, errors.New("discovery_unavailable")
}

type dahua struct{}

func (dahua) ManagesProfiles() bool { return true }

func (dahua) Validate(s Source) error {
	return validateManagedSource(s)
}

// Vendor adapters share access fields and public profile identifiers. Discovery
// and private RTSP numbering remain the responsibility of each adapter.
func validateManagedSource(s Source) error {
	c := s.Connection
	if c == nil || len(c.Host) > 253 || c.Host == "" || strings.ContainsAny(c.Host, "/@?#\\ \t\r\n") || (c.Protocol != "http" && c.Protocol != "https") || c.HTTPPort < 1 || c.HTTPPort > 65535 || c.RTSPPort < 0 || c.RTSPPort > 65535 || c.Username == "" || len(c.Username) > 200 || strings.ContainsAny(c.Username, "\r\n") || c.Password == nil || len(*c.Password) > 2000 {
		return errors.New("invalid_connection")
	}
	if strings.Contains(c.Host, ":") && net.ParseIP(c.Host) == nil {
		return errors.New("invalid_connection")
	}
	if net.ParseIP(c.Host) == nil {
		for _, label := range strings.Split(c.Host, ".") {
			if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				return errors.New("invalid_connection")
			}
			for _, r := range label {
				if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
					return errors.New("invalid_connection")
				}
			}
		}
	}
	for _, p := range s.Profiles {
		if !managedProfilePattern.MatchString(p.ID) || p.URL != "" {
			return errors.New("invalid_profiles")
		}
	}
	return nil
}
func (dahua) Resolve(s Source, id string) (string, error) {
	if s.Connection == nil || s.Connection.RTSPPort == 0 || len(s.Profiles) == 0 {
		return "", errors.New("camera_discovery_required")
	}
	found := false
	for _, p := range s.Profiles {
		if p.ID == id {
			found = true
			break
		}
	}
	if !found || s.Connection == nil {
		return "", errors.New("profile_unavailable")
	}
	subtype := 0
	if id != "main" {
		var e error
		subtype, e = strconv.Atoi(strings.TrimPrefix(id, "sub"))
		if e != nil {
			return "", errors.New("profile_unavailable")
		}
	}
	c := s.Connection
	pass := ""
	if c.Password != nil {
		pass = *c.Password
	}
	u := url.URL{Scheme: "rtsp", Host: net.JoinHostPort(c.Host, strconv.Itoa(c.RTSPPort)), Path: "/cam/realmonitor", User: url.UserPassword(c.Username, pass)}
	u.RawQuery = "channel=1&subtype=" + strconv.Itoa(subtype)
	return u.String(), nil
}
