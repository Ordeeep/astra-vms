package camera

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDahuaDiscoveryRespectsMaximumExtraStreams(t *testing.T) {
	for _, tc := range []struct {
		name, definition, caps string
		want                   int
		failure                bool
	}{
		{"two ignores enabled placeholder", "table.MaxExtraStream=2", "", 3, false},
		{"three retains real third stream", "table.MaxExtraStream=3", "", 4, false},
		{"main only", "table.MaxExtraStream=0", "", 1, false},
		{"alternative capabilities", "", "caps.MaxExtraStream=2", 3, false},
		{"invalid primary uses alternative", "table.MaxExtraStream=invalid", `{"caps":{"MaxExtraStream":3}}`, 4, false},
		{"both unavailable", "", "", 0, true},
		{"invalid limits", "table.MaxExtraStream=-1", "caps.MaxExtraStream=100", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Error("discovery wrote to camera")
				}
				switch {
				case r.URL.Query().Get("name") == "Encode":
					fmt.Fprintln(w, "table.Encode[0].MainFormat[0].Video.Compression=H.265")
					for i := 0; i < 3; i++ {
						fmt.Fprintf(w, "table.Encode[0].ExtraFormat[%d].Video.Compression=H.265\ntable.Encode[0].ExtraFormat[%d].VideoEnable=true\n", i, i)
					}
				case r.URL.Query().Get("name") == "RTSP":
					fmt.Fprint(w, "table.RTSP.Port=554")
				case r.URL.Query().Get("name") == "MaxExtraStream" && tc.definition != "":
					fmt.Fprint(w, tc.definition)
				case r.URL.Path == "/cgi-bin/encode.cgi" && tc.caps != "":
					fmt.Fprint(w, tc.caps)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			result, err := Discover(context.Background(), fixtureSource(t, server.URL))
			if tc.failure {
				if err == nil {
					t.Fatal("unconfirmed capabilities were treated as valid profiles")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Profiles) != tc.want {
				t.Fatalf("profiles=%d, want %d", len(result.Profiles), tc.want)
			}
			if result.Profiles[0].ID != "main" {
				t.Fatal("main profile lost")
			}
			for i, p := range result.Profiles[1:] {
				if p.ID != fmt.Sprintf("sub%d", i+1) {
					t.Fatal("profile numbering changed")
				}
			}
		})
	}
}
