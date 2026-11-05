package camera

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPTZHikvisionDigestMovementAndStop(t *testing.T) {
	var writes []PTZMove
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="PTZ", nonce="fixture", qop="auth"`)
			w.WriteHeader(401)
			return
		}
		p, err := digestParams(r.Header.Get("Authorization"))
		hash := func(s string) string { v := md5.Sum([]byte(s)); return hex.EncodeToString(v[:]) }
		want := hash(hash("operator:PTZ:p@ss:/?&\"word") + ":fixture:" + p["nc"] + ":" + p["cnonce"] + ":auth:" + hash(r.Method+":"+r.URL.RequestURI()))
		if err != nil || p["response"] != want {
			t.Error("Digest did not sign the actual HTTP method")
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /ISAPI/PTZCtrl/channels/1/capabilities":
			fmt.Fprint(w, `<PTZChanelCap><ContinuousPanTiltSpace/><ContinuousZoomSpace/><homePostionSupport>true</homePostionSupport></PTZChanelCap>`)
		case "PUT /ISAPI/PTZCtrl/channels/1/continuous":
			var data struct {
				Pan  int `xml:"pan"`
				Tilt int `xml:"tilt"`
				Zoom int `xml:"zoom"`
			}
			if xml.Unmarshal(raw, &data) != nil || r.Header.Get("Content-Type") != "application/xml" {
				t.Error("PUT body was not replayed after Digest challenge")
			}
			writes = append(writes, PTZMove{Pan: float64(data.Pan), Tilt: float64(data.Tilt), Zoom: float64(data.Zoom)})
			fmt.Fprint(w, `<ResponseStatus><statusCode>1</statusCode><subStatusCode>ok</subStatusCode></ResponseStatus>`)
		default:
			t.Error("unexpected device endpoint", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	s := hikvisionFixtureSource(t, server.URL)
	driver, err := OpenPTZ(s)
	if err != nil {
		t.Fatal(err)
	}
	caps, err := driver.Capabilities(context.Background())
	if err != nil || !caps.PanTilt || !caps.Zoom || !caps.Home {
		t.Fatalf("caps: %+v %v", caps, err)
	}
	if err = driver.Move(context.Background(), PTZMove{Pan: -.6, Tilt: .6}); err != nil {
		t.Fatal(err)
	}
	if err = driver.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(writes) != 2 || writes[0] != (PTZMove{Pan: -60, Tilt: 60}) || writes[1] != (PTZMove{}) {
		t.Fatalf("wrong movement/stop: %+v", writes)
	}
}

func TestPTZHikvisionPartialResourcesHomeAndDeviceErrors(t *testing.T) {
	for _, tc := range []struct {
		name, caps, response, want string
		home                       bool
	}{
		{"zoom only", `<PTZChanelCap><ContinuousZoomSpace/></PTZChanelCap>`, `<ResponseStatus><statusCode>1</statusCode></ResponseStatus>`, "", false},
		{"home", `<PTZChanelCap><ContinuousPanTiltSpace/><homePostionSupport>true</homePostionSupport></PTZChanelCap>`, `<ResponseStatus><statusCode>0</statusCode></ResponseStatus>`, "", true},
		{"HTTP 200 permission error", `<PTZChanelCap/>`, `<ResponseStatus><statusCode>4</statusCode><subStatusCode>noPermission</subStatusCode></ResponseStatus>`, "camera_auth_failed", false},
		{"trailing XML", `<PTZChanelCap/>`, `<ResponseStatus><statusCode>1</statusCode></ResponseStatus><secret/>`, "camera_api_response_invalid", false},
		{"wrong XML", `<PTZChanelCap/>`, `<Other><statusCode>1</statusCode></Other>`, "camera_api_response_invalid", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					fmt.Fprint(w, tc.caps)
					return
				}
				if tc.home && (r.URL.Path != "/ISAPI/PTZCtrl/channels/1/homeposition/goto" || r.URL.RawQuery != "" || r.ContentLength > 0) {
					t.Error("Home overwrote configuration or guessed a direction")
				}
				fmt.Fprint(w, tc.response)
			}))
			defer server.Close()
			d, err := OpenPTZ(hikvisionFixtureSource(t, server.URL))
			if err != nil {
				t.Fatal(err)
			}
			caps, err := d.Capabilities(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "zoom only" && (caps.PanTilt || !caps.Zoom || caps.Home) {
				t.Fatal("invented resources", caps)
			}
			if tc.home {
				err = d.Home(context.Background())
			} else {
				err = d.Stop(context.Background())
			}
			if Code(err) != tc.want {
				t.Fatalf("got %v want %s", err, tc.want)
			}
		})
	}
	for _, kind := range []string{"rtsp", "dahua"} {
		if _, err := OpenPTZ(Source{Type: kind}); Code(err) != "ptz_unsupported" {
			t.Fatalf("unimplemented adapter %s: %v", kind, err)
		}
	}
}
