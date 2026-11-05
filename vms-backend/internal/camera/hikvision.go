package camera

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// This adapter addresses a single IP camera, channel 1. Recorder channels,
// activation and configuration writes remain outside its scope.
type hikvision struct{}

func (hikvision) ManagesProfiles() bool { return true }

func (hikvision) Validate(s Source) error {
	if err := validateManagedSource(s); err != nil {
		return err
	}
	for _, p := range s.Profiles {
		if _, err := hikvisionStreamID(p.ID); err != nil {
			return errors.New("invalid_profiles")
		}
	}
	return nil
}

func hikvisionStreamID(id string) (int, error) {
	if id == "main" {
		return 101, nil
	}
	if !managedProfilePattern.MatchString(id) {
		return 0, errors.New("profile_unavailable")
	}
	n, err := strconv.Atoi(strings.TrimPrefix(id, "sub"))
	if err != nil || n < 1 || n > 98 {
		return 0, errors.New("profile_unavailable")
	}
	return 101 + n, nil
}

func (hikvision) Resolve(s Source, id string) (string, error) {
	if s.Connection == nil || s.Connection.RTSPPort == 0 || len(s.Profiles) == 0 {
		return "", errors.New("camera_discovery_required")
	}
	stream, err := hikvisionStreamID(id)
	if err != nil {
		return "", err
	}
	found := false
	for _, p := range s.Profiles {
		if p.ID == id {
			found = true
			break
		}
	}
	if !found {
		return "", errors.New("profile_unavailable")
	}
	c := s.Connection
	password := ""
	if c.Password != nil {
		password = *c.Password
	}
	u := url.URL{Scheme: "rtsp", Host: net.JoinHostPort(c.Host, strconv.Itoa(c.RTSPPort)), Path: "/ISAPI/Streaming/channels/" + strconv.Itoa(stream), User: url.UserPassword(c.Username, password)}
	return u.String(), nil
}

func (hikvision) Discover(ctx context.Context, s Source) (Discovery, error) {
	if s.Connection == nil {
		return Discovery{}, errors.New("invalid_connection")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, ResponseHeaderTimeout: 3 * time.Second, MaxResponseHeaderBytes: 64 << 10, IdleConnTimeout: 10 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	read := func(path string, dest any) error {
		raw, err := digestGet(ctx, client, *s.Connection, path)
		if err != nil {
			return err
		}
		return hikvisionXML(raw, dest)
	}
	var channels hikvisionChannels
	if err := read("/ISAPI/Streaming/channels", &channels); err != nil {
		return Discovery{}, err
	}
	profiles, err := channels.profiles()
	if err != nil {
		return Discovery{}, err
	}
	var ports hikvisionPorts
	if err := read("/ISAPI/Security/adminAccesses", &ports); err != nil {
		return Discovery{}, err
	}
	port := 0
	for _, p := range ports.Protocols {
		if !strings.EqualFold(strings.TrimSpace(p.Protocol), "RTSP") {
			continue
		}
		if p.Enabled != nil && !*p.Enabled {
			return Discovery{}, errors.New("camera_api_unsupported")
		}
		if port != 0 || p.Port < 1 || p.Port > 65535 {
			return Discovery{}, errors.New("camera_api_response_invalid")
		}
		port = p.Port
	}
	if port == 0 {
		return Discovery{}, errors.New("camera_api_response_invalid")
	}
	result := Discovery{Profiles: profiles, RTSPPort: port}
	// Identification is optional and never gates a usable stream configuration.
	var device struct {
		XMLName      xml.Name `xml:"DeviceInfo"`
		Model        string   `xml:"model"`
		Serial       string   `xml:"serialNumber"`
		Firmware     string   `xml:"firmwareVersion"`
		Manufacturer string   `xml:"manufacturer"`
	}
	if ctx.Err() == nil && read("/ISAPI/System/deviceInfo", &device) == nil {
		clean := func(value string) string {
			value = strings.TrimSpace(value)
			if len(value) > 256 || strings.ContainsAny(value, "\r\n") {
				return ""
			}
			return value
		}
		result.Device = Device{Model: clean(device.Model), Serial: clean(device.Serial), Firmware: clean(device.Firmware), Vendor: clean(device.Manufacturer)}
	}
	return result, nil
}

type hikvisionChannels struct {
	XMLName  xml.Name `xml:"StreamingChannelList"`
	Channels []struct {
		ID      int   `xml:"id"`
		Enabled *bool `xml:"enabled"`
		Video   *struct {
			Enabled   *bool  `xml:"enabled"`
			Input     *int   `xml:"videoInputChannelID"`
			Codec     string `xml:"videoCodecType"`
			Width     int    `xml:"videoResolutionWidth"`
			Height    int    `xml:"videoResolutionHeight"`
			FrameRate int    `xml:"maxFrameRate"`
		} `xml:"Video"`
	} `xml:"StreamingChannel"`
}

func (channels hikvisionChannels) profiles() ([]Profile, error) {
	profiles := []Profile{}
	seen := map[int]bool{}
	for _, ch := range channels.Channels {
		// Skip other camera channels and entries that contain audio only.
		if ch.ID < 101 || ch.ID > 199 || ch.Video == nil {
			continue
		}
		v := ch.Video
		if seen[ch.ID] || (v.Input != nil && *v.Input != 1) || len(v.Codec) > 32 || v.Width < 0 || v.Width > 32768 || v.Height < 0 || v.Height > 32768 || v.FrameRate < 0 || v.FrameRate > 100000 {
			return nil, errors.New("camera_api_response_invalid")
		}
		seen[ch.ID] = true
		p := Profile{ID: "main", Name: "Principal", Codec: strings.ToLower(strings.TrimSpace(v.Codec)), Width: v.Width, Height: v.Height, FPS: float64(v.FrameRate) / 100}
		if ch.ID != 101 {
			n := strconv.Itoa(ch.ID - 101)
			p.ID, p.Name = "sub"+n, "Substream "+n
		}
		if ch.Enabled != nil || v.Enabled != nil {
			enabled := (ch.Enabled == nil || *ch.Enabled) && (v.Enabled == nil || *v.Enabled)
			p.Enabled = &enabled
		}
		profiles = append(profiles, p)
	}
	if !seen[101] || len(profiles) > 16 {
		return nil, errors.New("camera_api_response_invalid")
	}
	sort.Slice(profiles, func(i, j int) bool {
		a, _ := hikvisionStreamID(profiles[i].ID)
		b, _ := hikvisionStreamID(profiles[j].ID)
		return a < b
	})
	return profiles, nil
}

type hikvisionPorts struct {
	XMLName   xml.Name `xml:"AdminAccessProtocolList"`
	Protocols []struct {
		Protocol string `xml:"protocol"`
		Port     int    `xml:"portNo"`
		Enabled  *bool  `xml:"enabled"`
	} `xml:"AdminAccessProtocol"`
}

// XML names are matched by local name: old and new firmware use different
// default namespaces. Reject trailing documents and never return device text in
// errors. ResponseStatus can carry an error even with HTTP 200.
func hikvisionXML(raw []byte, dest any) error {
	var status struct {
		XMLName   xml.Name
		SubStatus string `xml:"subStatusCode"`
	}
	if xml.Unmarshal(raw, &status) != nil {
		return errors.New("camera_api_response_invalid")
	}
	if status.XMLName.Local == "ResponseStatus" {
		switch strings.ToLower(strings.TrimSpace(status.SubStatus)) {
		case "notauthorized", "nopermission", "unauthorized", "badusernameorpassword":
			return errors.New("camera_auth_failed")
		default:
			return errors.New("camera_api_unsupported")
		}
	}
	decoder := xml.NewDecoder(strings.NewReader(string(raw)))
	if decoder.Decode(dest) != nil {
		return errors.New("camera_api_response_invalid")
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return errors.New("camera_api_response_invalid")
		}
		if data, ok := token.(xml.CharData); !ok || strings.TrimSpace(string(data)) != "" {
			return errors.New("camera_api_response_invalid")
		}
	}
}
