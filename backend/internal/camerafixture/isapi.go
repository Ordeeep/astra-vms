// Package camerafixture provides the loopback ISAPI camera used in the development pilot.
package camerafixture

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

type Device struct {
	Line7Enabled atomic.Bool
	Interval     time.Duration
}

func (d *Device) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	switch r.URL.Path {
	case "/ISAPI/System/deviceInfo":
		fmt.Fprint(w, "<DeviceInfo><model>Astra ISAPI Simulator</model><firmwareVersion>fixture-1</firmwareVersion></DeviceInfo>")
	case "/ISAPI/Streaming/channels":
		fmt.Fprint(w, "<StreamingChannelList><StreamingChannel><id>101</id><Video><videoCodecType>H.264</videoCodecType></Video></StreamingChannel></StreamingChannelList>")
	case "/ISAPI/Security/adminAccesses":
		fmt.Fprint(w, "<AdminAccessProtocolList><AdminAccessProtocol><protocol>RTSP</protocol><portNo>554</portNo></AdminAccessProtocol></AdminAccessProtocolList>")
	case "/ISAPI/Smart/FieldDetection/1":
		fmt.Fprint(w, "<FieldDetection><id>1</id><enabled>true</enabled><FieldDetectionRegionList><FieldDetectionRegion><id>1</id><enabled>true</enabled></FieldDetectionRegion></FieldDetectionRegionList></FieldDetection>")
	case "/ISAPI/Smart/LineDetection":
		fmt.Fprintf(w, "<LineDetectionList><LineDetection><id>1</id><enabled>true</enabled><LineItemList><LineItem><id>7</id><enabled>%t</enabled></LineItem><LineItem><id>8</id><enabled>true</enabled></LineItem></LineItemList></LineDetection></LineDetectionList>", d.Line7Enabled.Load())
	case "/ISAPI/Event/notification/alertStream":
		d.stream(w, r)
	default:
		http.NotFound(w, r)
	}
}
func (d *Device) stream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "multipart/mixed; boundary=camera-events")
	fmt.Fprint(w, "--camera-events\r\n")
	part := func(kind, id, state, name string) error {
		raw := fmt.Sprintf("<EventNotificationAlert><eventType>%s</eventType><channelID>1</channelID><eventState>%s</eventState><dateTime>%s</dateTime><ruleID>%s</ruleID><ruleName>%s</ruleName></EventNotificationAlert>", kind, state, time.Now().UTC().Format(time.RFC3339Nano), id, name)
		_, err := fmt.Fprintf(w, "Content-Type: application/xml\r\nContent-Length: %d\r\n\r\n%s\r\n--camera-events\r\n", len(raw), raw)
		return err
	}
	alerts := time.NewTicker(d.Interval)
	defer alerts.Stop()
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()
	for {
		for _, rule := range []struct{ kind, id, name string }{{"fielddetection", "1", "Pátio"}, {"linedetection", "8", "Garagem"}, {"linedetection", "7", "Portão"}} {
			if rule.id == "7" && !d.Line7Enabled.Load() {
				continue
			}
			for _, state := range []string{"active", "inactive"} {
				if part(rule.kind, rule.id, state, rule.name) != nil {
					return
				}
			}
		}
		if http.NewResponseController(w).Flush() != nil {
			return
		}
	idle:
		for {
			select {
			case <-r.Context().Done():
				return
			case <-alerts.C:
				break idle
			case <-heartbeat.C:
				if part("heartBeat", "", "", "") != nil || http.NewResponseController(w).Flush() != nil {
					return
				}
			}
		}
	}
}
