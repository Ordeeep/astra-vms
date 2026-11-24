package camera

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHikvisionEventSubscriptionAndMultipart(t *testing.T) {
	const capXML = `<SubscribeEventCap><heartbeat min="10" max="60"/><EventList><Event><type opt="fielddetection,linedetection"/></Event></EventList></SubscribeEventCap>`
	client := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="camera", nonce="one", qop="auth"`)
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/ISAPI/System/capabilities":
			fmt.Fprint(w, "<DeviceCap><EventCap><isSupportSubscribeEvent>true</isSupportSubscribeEvent></EventCap></DeviceCap>")
		case "/ISAPI/Event/notification/subscribeEventCap":
			fmt.Fprint(w, capXML)
		case "/ISAPI/Event/notification/subscribeEvent":
			body, _ := io.ReadAll(r.Body)
			if r.Method != "POST" || !strings.Contains(string(body), "<type>linedetection</type>") {
				t.Error("invalid subscription")
			}
			w.Header().Set("Content-Type", "multipart/form-data; boundary=abc")
			// This firmware counts the CRLF before the next boundary in Content-Length.
			line := "<EventNotificationAlert><eventType>linedetection</eventType><eventState>active</eventState></EventNotificationAlert>"
			fmt.Fprintf(w, "--abc\r\nContent-Type: application/xml\r\nContent-Length: %d\r\n\r\n%s\r\n", len(line)+2, line)
			for _, part := range []struct{ kind, value string }{
				{"application/xml", "<SubscribeEventResponse><id>1</id></SubscribeEventResponse>"},
				{"image/jpeg", "binary fixture"},
				{"application/xml", "<EventNotificationAlert><eventType>heartBeat</eventType></EventNotificationAlert>"},
				{"application/json", `{"EventNotificationAlert":{"eventType":"fielddetection","eventState":"inactive","ruleID":4,"dateTime":"2026-11-24T09:15:00-03:00"}}`},
			} {
				fmt.Fprintf(w, "--abc\r\nContent-Type: %s\r\nContent-Length: %d\r\n\r\n%s\r\n", part.kind, len(part.value), part.value)
			}
			fmt.Fprint(w, "--abc--\r\n")
		default:
			t.Errorf("unexpected call %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer client.Close()
	u, _ := url.Parse(client.URL)
	port, _ := strconv.Atoi(u.Port())
	pass := "camera-test"
	source := Source{Connection: &Connection{Host: u.Hostname(), Protocol: "http", HTTPPort: port, Username: "fixture", Password: &pass}}
	ctx, done := context.WithTimeout(context.Background(), 3*time.Second)
	defer done()
	var seen []*CameraAlert
	err := (hikvision{}).Receive(ctx, source, func(a *CameraAlert) error { seen = append(seen, a); return nil })
	if err != io.EOF || len(seen) != 3 || seen[0] == nil || seen[0].Kind != "line_crossing" || seen[1] != nil || seen[2] == nil || seen[2].Kind != "intrusion" || seen[2].State != "inactive" || seen[2].RuleID != "4" || seen[2].CameraAt == nil {
		t.Fatalf("unexpected events or framing: %v %v", seen, err)
	}
}
