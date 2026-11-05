package camera

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureSource(t *testing.T, endpoint string) Source {
	t.Helper()
	u, e := url.Parse(endpoint)
	if e != nil {
		t.Fatal(e)
	}
	port, _ := strconv.Atoi(u.Port())
	pass := "p@ss:/?&\"word"
	s, e := Normalize(Source{Type: "dahua", Connection: &Connection{Host: u.Hostname(), Protocol: u.Scheme, HTTPPort: port, RTSPPort: 554, Username: "operator", Password: &pass}, Profiles: []Profile{{ID: "main", Name: "Principal"}, {ID: "sub1", Name: "Substream 1"}}})
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestDahuaDigestDiscoveryRenewedNonceAndConfiguredProfiles(t *testing.T) {
	var renewed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || strings.Contains(r.URL.RawQuery, "setConfig") {
			t.Error("write request")
		}
		authorization := r.Header.Get("Authorization")
		challenge := func(nonce string) {
			w.Header().Set("WWW-Authenticate", `Digest realm="camera, lab", nonce="`+nonce+`", qop="auth,auth-int", algorithm=SHA-256`)
			w.WriteHeader(401)
		}
		if authorization == "" {
			challenge("first")
			return
		}
		p, e := digestParams(authorization)
		if e != nil {
			t.Error(e)
			w.WriteHeader(403)
			return
		}
		if p["nonce"] == "first" && !renewed.Swap(true) {
			challenge("renewed")
			return
		}
		h := func(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
		expected := h(h("operator:camera, lab:p@ss:/?&\"word") + ":" + p["nonce"] + ":" + p["nc"] + ":" + p["cnonce"] + ":auth:" + h("GET:"+r.URL.RequestURI()))
		if p["response"] != expected || p["uri"] != r.URL.RequestURI() || p["username"] != "operator" {
			t.Error("invalid Digest response")
			w.WriteHeader(403)
			return
		}
		if r.URL.Query().Get("name") == "RTSP" {
			fmt.Fprint(w, "table.RTSP.Port=8554")
			return
		}
		if r.URL.Query().Get("name") == "MaxExtraStream" {
			fmt.Fprint(w, "table.MaxExtraStream=1")
			return
		}
		if r.URL.Query().Get("name") == "Encode" {
			fmt.Fprint(w, "table.Encode[0].MainFormat[0].Video.Compression=H.265\ntable.Encode[0].MainFormat[0].Video.Width=1920\ntable.Encode[0].MainFormat[0].Video.Height=1080\ntable.Encode[0].MainFormat[0].Video.FPS=25\ntable.Encode[0].MainFormat[0].VideoEnable=true\ntable.Encode[0].MainFormat[1].Video.Compression=H.264\ntable.Encode[0].ExtraFormat[0].Video.Compression=H.264\ntable.Encode[0].ExtraFormat[0].VideoEnable=false\ntable.Encode[1].MainFormat[0].Video.Compression=MJPEG")
			return
		}
		if r.URL.Query().Get("action") == "getDeviceType" {
			fmt.Fprint(w, "type=IPC-Test")
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()
	s := fixtureSource(t, server.URL)
	result, e := Discover(context.Background(), s)
	if e != nil {
		t.Fatal(e)
	}
	if !renewed.Load() || len(result.Profiles) != 2 || result.Profiles[0].ID != "main" || result.Profiles[1].ID != "sub1" || result.RTSPPort != 8554 || result.Device.Model != "IPC-Test" || result.Device.Serial != "" {
		t.Fatalf("bad discovery %+v", result)
	}
	if result.Profiles[0].Width != 1920 || result.Profiles[0].Codec != "h.265" || result.Profiles[1].Enabled == nil || *result.Profiles[1].Enabled {
		t.Fatal("wrong configuration metadata")
	}
	raw, _ := json.Marshal(Public(s))
	if strings.Contains(string(raw), "password\"") || strings.Contains(string(raw), "p@ss") {
		t.Fatal("public secret")
	}
	address, e := Resolve(s, "sub1")
	if e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(address)
	pass, _ := u.User.Password()
	if pass != *s.Connection.Password || u.Query().Get("channel") != "1" || u.Query().Get("subtype") != "1" {
		t.Fatal("incorrect private RTSP origin")
	}
	// Resolution uses known configuration after the API has gone away.
	server.Close()
	if _, e = Resolve(s, "main"); e != nil {
		t.Fatal(e)
	}
}
func TestDigestMD5WithoutQopAndUnsupportedChallenges(t *testing.T) {
	pass := "secret"
	c := Connection{Username: "user", Password: &pass}
	authorization, e := digestAuthorization(`Digest realm="r",nonce="n"`, c, "/read")
	if e != nil {
		t.Fatal(e)
	}
	p, e := digestParams(authorization)
	if e != nil {
		t.Fatal(e)
	}
	h := func(s string) string { v := md5.Sum([]byte(s)); return hex.EncodeToString(v[:]) }
	if p["response"] != h(h("user:r:secret")+":n:"+h("GET:/read")) {
		t.Fatal("incorrect legacy Digest")
	}
	for _, challenge := range []string{`Basic realm="r"`, `Digest realm="r"`, `Digest realm="r",nonce="unterminated`, `Digest realm="r",nonce="n",algorithm=unknown`, `Digest realm="r",nonce="n",qop="auth-int"`} {
		if _, e = digestAuthorization(challenge, c, "/"); e == nil {
			t.Fatalf("accepted %s", challenge)
		}
	}
}
func TestDahuaAPIErrorBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		challenge string
		body      string
		code      string
	}{
		{"unauthorized", 401, `Digest realm="r",nonce="n",qop="auth"`, "private details", "camera_auth_failed"},
		{"unsupported", 404, "", "private details", "camera_api_unsupported"},
		{"invalid", 200, "", "OK", "camera_api_response_invalid"},
		{"oversized", 200, "", strings.Repeat("x", (1<<20)+1), "camera_api_response_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("WWW-Authenticate", tc.challenge)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			_, e := Discover(context.Background(), fixtureSource(t, server.URL))
			if Code(e) != tc.code {
				t.Fatalf("expected %s got %v", tc.code, e)
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, e := Discover(ctx, fixtureSource(t, server.URL)); Code(e) != "camera_api_timeout" {
		t.Fatal(e)
	}
}
func TestJSONEncodeAndSourceValidation(t *testing.T) {
	raw := []byte(`{"table":{"Encode":[{"MainFormat":[{"Video":{"Compression":"H.264","Width":640,"FPS":15},"VideoEnable":true},{"Video":{"Compression":"H.265"}}],"ExtraFormat":[{"Video":{"Compression":"H.265"}}]}]}}`)
	p := parseProfiles(parseValues(raw))
	if len(p) != 2 || p[0].Width != 640 || p[1].ID != "sub1" {
		t.Fatal(p)
	}
	s := fixtureSource(t, "http://127.0.0.1:80")
	for _, host := range []string{"http://camera", "camera/path", "camera:80", "a@b", "a b", "-camera", "camera..test"} {
		s.Connection.Host = host
		if _, e := Normalize(s); e == nil {
			t.Fatalf("accepted host %s", host)
		}
	}
	s = fixtureSource(t, "http://127.0.0.1:80")
	s.Profiles = append(s.Profiles, Profile{ID: "main", Name: "duplicate"})
	if _, e := Normalize(s); e == nil {
		t.Fatal("duplicate profile")
	}
	legacy, e := Normalize(DecodeLegacy([]Profile{{ID: "main", Name: "Main", URL: "rtsp://user:password@camera/video"}}))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Resolve(legacy, "main"); e != nil {
		t.Fatal(e)
	}
}

func TestDahuaRequiresActualRTSPPortAndSkipsAudioOnlyFormats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") == "MaxExtraStream" {
			fmt.Fprint(w, "table.MaxExtraStream=1")
			return
		}
		if r.URL.Query().Get("name") == "Encode" {
			fmt.Fprint(w, "table.Encode[0].MainFormat[0].Video.Compression=H.264\ntable.Encode[0].ExtraFormat[0].AudioEnable=true")
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	if _, e := Discover(context.Background(), fixtureSource(t, server.URL)); Code(e) != "camera_api_unsupported" {
		t.Fatal("missing RTSP configuration silently used a default")
	}
	profiles := parseProfiles(parseValues([]byte("table.Encode[0].MainFormat[0].Video.Compression=H.264\ntable.Encode[0].ExtraFormat[0].AudioEnable=true")))
	if len(profiles) != 1 || profiles[0].ID != "main" {
		t.Fatal("audio-only settings invented an additional video stream")
	}
}
