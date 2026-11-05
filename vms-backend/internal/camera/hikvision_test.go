package camera

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const hikvisionChannelFixture = `<StreamingChannelList xmlns="http://www.hikvision.com/ver20/XMLSchema" version="2.0">
 <StreamingChannel><id>103</id><enabled>true</enabled><Video><enabled>false</enabled><videoInputChannelID>1</videoInputChannelID><videoCodecType>H.264</videoCodecType><maxFrameRate>1250</maxFrameRate></Video></StreamingChannel>
 <StreamingChannel><id>201</id><Video><videoInputChannelID>2</videoInputChannelID><videoCodecType>H.265</videoCodecType></Video></StreamingChannel>
 <StreamingChannel><id>101</id><enabled>true</enabled><Video><enabled>true</enabled><videoInputChannelID>1</videoInputChannelID><videoCodecType>H.265</videoCodecType><videoResolutionWidth>2560</videoResolutionWidth><videoResolutionHeight>1440</videoResolutionHeight><maxFrameRate>2997</maxFrameRate></Video></StreamingChannel>
 <StreamingChannel><id>104</id><Audio><enabled>true</enabled></Audio></StreamingChannel>
</StreamingChannelList>`
const hikvisionPortFixture = `<AdminAccessProtocolList xmlns="http://www.isapi.org/ver20/XMLSchema"><AdminAccessProtocol><protocol>HTTP</protocol><portNo>80</portNo></AdminAccessProtocol><AdminAccessProtocol><enabled>true</enabled><protocol>RTSP</protocol><portNo>8554</portNo></AdminAccessProtocol></AdminAccessProtocolList>`

func hikvisionFixtureSource(t *testing.T, endpoint string) Source {
	t.Helper()
	s := fixtureSource(t, endpoint)
	s.Type, s.Profiles = "hikvision", nil
	s.Connection.RTSPPort = 0
	s, err := Normalize(s)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestHikvisionDigestDiscoveryAndPrivateOrigins(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("discovery wrote to device")
		}
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="IP Camera", nonce="fixture", algorithm=MD5, qop="auth"`)
			w.WriteHeader(401)
			return
		}
		p, err := digestParams(r.Header.Get("Authorization"))
		h := func(s string) string { v := md5.Sum([]byte(s)); return hex.EncodeToString(v[:]) }
		want := h(h("operator:IP Camera:p@ss:/?&\"word") + ":fixture:" + p["nc"] + ":" + p["cnonce"] + ":auth:" + h("GET:"+r.URL.RequestURI()))
		if err != nil || p["username"] != "operator" || p["uri"] != r.URL.RequestURI() || p["response"] != want {
			t.Error("invalid Digest authentication")
			w.WriteHeader(403)
			return
		}
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/ISAPI/Streaming/channels":
			fmt.Fprint(w, hikvisionChannelFixture)
		case "/ISAPI/Security/adminAccesses":
			fmt.Fprint(w, hikvisionPortFixture)
		case "/ISAPI/System/deviceInfo":
			fmt.Fprint(w, `<DeviceInfo xmlns="http://www.isapi.org/ver20/XMLSchema"><model>DS-fixture</model><serialNumber>synthetic</serialNumber><firmwareVersion>V-test</firmwareVersion><manufacturer>Hikvision</manufacturer></DeviceInfo>`)
		default:
			t.Error("unexpected endpoint")
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	s := hikvisionFixtureSource(t, server.URL)
	d, err := Discover(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 3 || d.RTSPPort != 8554 || d.Device.Model != "DS-fixture" || d.Device.Serial != "synthetic" || d.Device.Vendor != "Hikvision" || d.Device.Firmware != "V-test" {
		t.Fatalf("bad discovery: %+v", d)
	}
	if len(d.Profiles) != 2 || d.Profiles[0].ID != "main" || d.Profiles[1].ID != "sub2" || d.Profiles[0].Codec != "h.265" || d.Profiles[0].FPS != 29.97 || d.Profiles[0].Width != 2560 || d.Profiles[0].Height != 1440 || d.Profiles[1].Enabled == nil || *d.Profiles[1].Enabled {
		t.Fatalf("bad streams: %+v", d.Profiles)
	}
	s.Profiles, s.Connection.RTSPPort = d.Profiles, d.RTSPPort
	for id, path := range map[string]string{"main": "/ISAPI/Streaming/channels/101", "sub2": "/ISAPI/Streaming/channels/103"} {
		address, err := Resolve(s, id)
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(address)
		if err != nil {
			t.Fatal(err)
		}
		password, _ := u.User.Password()
		if u.Path != path || u.Port() != "8554" || password != *s.Connection.Password || u.User.Username() != "operator" {
			t.Fatal("incorrect private RTSP origin")
		}
	}
	if _, err := Resolve(s, "sub1"); Code(err) != "profile_unavailable" {
		t.Fatal("invented absent substream")
	}
	public, _ := json.Marshal(Public(s))
	if strings.Contains(string(public), "password\"") || strings.Contains(string(public), "rtsp://") || strings.Contains(string(public), "rtspPort") {
		t.Fatal("private source data leaked")
	}
	server.Close()
	if _, err := Resolve(s, "main"); err != nil {
		t.Fatal("known origin depends on API availability")
	}
}

func TestHikvisionDiscoveryBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, channels, ports, code string
		status                      int
	}{
		{name: "main only", channels: `<StreamingChannelList><StreamingChannel><id>101</id><Video><videoCodecType>H.264</videoCodecType></Video></StreamingChannel></StreamingChannelList>`, ports: hikvisionPortFixture},
		{name: "no main", channels: strings.ReplaceAll(hikvisionChannelFixture, "<id>101</id>", "<id>102</id>"), code: "camera_api_response_invalid"},
		{name: "duplicate", channels: strings.ReplaceAll(hikvisionChannelFixture, "<id>103</id>", "<id>101</id>"), code: "camera_api_response_invalid"},
		{name: "input mismatch", channels: strings.ReplaceAll(hikvisionChannelFixture, "<videoInputChannelID>1</videoInputChannelID>", "<videoInputChannelID>2</videoInputChannelID>"), code: "camera_api_response_invalid"},
		{name: "bad dimensions", channels: strings.ReplaceAll(hikvisionChannelFixture, "2560", "-1"), code: "camera_api_response_invalid"},
		{name: "bad fps", channels: strings.ReplaceAll(hikvisionChannelFixture, "2997", "NaN"), code: "camera_api_response_invalid"},
		{name: "missing port", channels: hikvisionChannelFixture, ports: `<AdminAccessProtocolList/>`, code: "camera_api_response_invalid"},
		{name: "bad port", channels: hikvisionChannelFixture, ports: strings.ReplaceAll(hikvisionPortFixture, "8554", "70000"), code: "camera_api_response_invalid"},
		{name: "disabled RTSP", channels: hikvisionChannelFixture, ports: strings.ReplaceAll(hikvisionPortFixture, "<enabled>true</enabled>", "<enabled>false</enabled>"), code: "camera_api_unsupported"},
		{name: "wrong root", channels: `<DeviceCap/>`, code: "camera_api_response_invalid"},
		{name: "broken XML", channels: `<StreamingChannelList>`, code: "camera_api_response_invalid"},
		{name: "multiple documents", channels: hikvisionChannelFixture + `<StreamingChannelList/>`, code: "camera_api_response_invalid"},
		{name: "XML error", channels: `<ResponseStatus><statusCode>4</statusCode><subStatusCode>notAuthorized</subStatusCode><statusString>private</statusString></ResponseStatus>`, code: "camera_auth_failed"},
		{name: "oversized", channels: strings.Repeat("x", (1<<20)+1), code: "camera_api_response_invalid"},
		{name: "unsupported", status: 404, code: "camera_api_unsupported"},
		{name: "forbidden", status: 403, code: "camera_auth_failed"},
		{name: "unauthorized", status: 401, code: "camera_auth_failed"},
		{name: "redirect", status: 302, code: "camera_api_unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status != 0 {
					w.Header().Set("WWW-Authenticate", `Digest realm="r",nonce="n",qop="auth"`)
					w.Header().Set("Location", "http://invalid.example/private")
					w.WriteHeader(tc.status)
					return
				}
				switch r.URL.Path {
				case "/ISAPI/Streaming/channels":
					fmt.Fprint(w, tc.channels)
				case "/ISAPI/Security/adminAccesses":
					fmt.Fprint(w, tc.ports)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			d, err := Discover(context.Background(), hikvisionFixtureSource(t, server.URL))
			if Code(err) != tc.code {
				t.Fatalf("want %s, got %v", tc.code, err)
			}
			if tc.code == "" && (len(d.Profiles) != 1 || d.Device.Model != "") {
				t.Fatal("optional identity changed usable profiles")
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := Discover(ctx, hikvisionFixtureSource(t, server.URL)); Code(err) != "camera_api_timeout" {
		t.Fatal(err)
	}
}

func TestHikvisionSourceValidationAndIPv6(t *testing.T) {
	s := hikvisionFixtureSource(t, "http://127.0.0.1:80")
	if !ManagedProfiles(s.Type) {
		t.Fatal("unregistered managed integration")
	}
	if _, err := Resolve(s, "main"); Code(err) != "camera_discovery_required" {
		t.Fatal("pending source resolved")
	}
	s.Profiles = []Profile{{ID: "main", Name: "Principal"}, {ID: "sub98", Name: "Substream 98"}}
	s.Connection.Host = "2001:db8::1"
	s.Connection.RTSPPort = 8554
	if _, err := Normalize(s); err != nil {
		t.Fatal(err)
	}
	origin, err := Resolve(s, "sub98")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(origin)
	if u.Host != "[2001:db8::1]:8554" || u.Path != "/ISAPI/Streaming/channels/199" {
		t.Fatal("incorrect IPv6 origin")
	}
	for _, id := range []string{"sub99", "sub01", "channel201"} {
		s.Profiles[1].ID = id
		if _, err := Normalize(s); Code(err) != "invalid_profiles" {
			t.Fatalf("accepted %s", id)
		}
	}
}
