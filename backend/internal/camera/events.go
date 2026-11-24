package camera

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"strings"
	"time"
)

// EventReceiver is an optional integration capability, independent of the video profiles.
type EventReceiver interface {
	EventDevice(context.Context, Source) (Device, error)
	Receive(context.Context, Source, func(*CameraAlert) error) error
	RuleInventory(context.Context, Source, string) (RuleInventory, error)
}

func (hikvision) EventDevice(ctx context.Context, s Source) (Device, error) {
	if s.Connection == nil {
		return Device{}, errors.New("invalid_connection")
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	client := eventClient()
	defer client.CloseIdleConnections()
	raw, err := digestGet(ctx, client, *s.Connection, "/ISAPI/System/deviceInfo")
	if err != nil {
		return Device{}, err
	}
	var d struct {
		XMLName  xml.Name `xml:"DeviceInfo"`
		Model    string   `xml:"model"`
		Firmware string   `xml:"firmwareVersion"`
	}
	if err = hikvisionXML(raw, &d); err != nil {
		return Device{}, err
	}
	if len(d.Model) > 256 || len(d.Firmware) > 256 {
		return Device{}, errors.New("camera_api_response_invalid")
	}
	return Device{Vendor: "Hikvision", Model: strings.TrimSpace(d.Model), Firmware: strings.TrimSpace(d.Firmware)}, nil
}

func Events(kind string) (EventReceiver, bool) { a, ok := registry[kind].(EventReceiver); return a, ok }

func EventIntegrations() []string {
	var kinds []string
	for kind := range registry {
		if _, ok := Events(kind); ok {
			kinds = append(kinds, kind)
		}
	}
	return kinds
}

type CameraAlert struct {
	Kind     string     `json:"kind"`
	State    string     `json:"state"`
	RuleID   string     `json:"ruleId,omitempty"`
	RuleName string     `json:"ruleName,omitempty"`
	CameraAt *time.Time `json:"cameraAt,omitempty"`
}
type DeviceRule struct {
	ID      string `xml:"id" json:"id"`
	Enabled *bool  `xml:"enabled" json:"enabled"`
}
type RuleInventory struct {
	Enabled  *bool        `json:"enabled"`
	Items    []DeviceRule `json:"items"`
	Complete bool         `json:"complete"`
}
type ruleChannel struct {
	ID      string `xml:"id"`
	Enabled *bool  `xml:"enabled"`
	Regions *struct {
		Items []DeviceRule `xml:"FieldDetectionRegion"`
	} `xml:"FieldDetectionRegionList"`
	Lines *struct {
		Items []DeviceRule `xml:"LineItem"`
	} `xml:"LineItemList"`
}

func eventClient() *http.Client {
	t := &http.Transport{DialContext: (&net.Dialer{Timeout: 4 * time.Second}).DialContext, ResponseHeaderTimeout: 6 * time.Second, IdleConnTimeout: 15 * time.Second, MaxResponseHeaderBytes: 64 << 10}
	return &http.Client{Transport: t, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func (hikvision) RuleInventory(ctx context.Context, s Source, kind string) (RuleInventory, error) {
	var result RuleInventory
	if s.Connection == nil {
		return result, errors.New("invalid_connection")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	client := eventClient()
	defer client.CloseIdleConnections()
	var channel ruleChannel
	switch kind {
	case "intrusion":
		raw, e := digestGet(ctx, client, *s.Connection, "/ISAPI/Smart/FieldDetection/1")
		if e != nil {
			return result, e
		}
		var doc struct {
			XMLName xml.Name `xml:"FieldDetection"`
			ruleChannel
		}
		if e = hikvisionXML(raw, &doc); e != nil {
			return result, e
		}
		channel = doc.ruleChannel
		if channel.Regions != nil {
			result.Items = channel.Regions.Items
			result.Complete = true
		}
	case "line_crossing":
		raw, e := digestGet(ctx, client, *s.Connection, "/ISAPI/Smart/LineDetection")
		if e != nil {
			return result, e
		}
		var doc struct {
			XMLName  xml.Name      `xml:"LineDetectionList"`
			Channels []ruleChannel `xml:"LineDetection"`
		}
		if e = hikvisionXML(raw, &doc); e != nil {
			return result, e
		}
		for _, item := range doc.Channels {
			if item.ID == "1" {
				channel = item
				break
			}
		}
		if channel.Lines != nil {
			result.Items = channel.Lines.Items
			result.Complete = true
		}
	default:
		return result, errors.New("unsupported_analytic_kind")
	}
	if channel.ID != "1" || channel.Enabled == nil {
		return RuleInventory{}, errors.New("camera_api_response_invalid")
	}
	seen := map[string]bool{}
	for _, item := range result.Items {
		if item.ID == "" || len(item.ID) > 128 || seen[item.ID] || item.Enabled == nil {
			return RuleInventory{}, errors.New("camera_api_response_invalid")
		}
		seen[item.ID] = true
	}
	result.Enabled = channel.Enabled
	return result, nil
}

func subscription(ctx context.Context, client *http.Client, c Connection) ([]byte, time.Duration) {
	const defaultSilence = 90 * time.Second
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, err := digestGet(ctx, client, c, "/ISAPI/System/capabilities")
	if err != nil {
		return nil, defaultSilence
	}
	decoder := xml.NewDecoder(strings.NewReader(string(raw)))
	for {
		t, e := decoder.Token()
		if e != nil {
			return nil, defaultSilence
		}
		el, ok := t.(xml.StartElement)
		if ok && el.Name.Local == "isSupportSubscribeEvent" {
			var supported bool
			if decoder.DecodeElement(&supported, &el) != nil || !supported {
				return nil, defaultSilence
			}
			break
		}
	}
	raw, err = digestGet(ctx, client, c, "/ISAPI/Event/notification/subscribeEventCap")
	if err != nil {
		return nil, defaultSilence
	}
	var caps struct {
		Types []struct {
			Value   string `xml:",chardata"`
			Options string `xml:"opt,attr"`
		} `xml:"EventList>Event>type"`
		Heartbeat *struct {
			Min int `xml:"min,attr"`
			Max int `xml:"max,attr"`
		} `xml:"heartbeat"`
	}
	if hikvisionXML(raw, &caps) != nil {
		return nil, defaultSilence
	}
	types := map[string]bool{}
	for _, t := range caps.Types {
		for _, v := range strings.Split(t.Options+","+t.Value, ",") {
			types[strings.TrimSpace(strings.ToLower(v))] = true
		}
	}
	if !types["fielddetection"] || !types["linedetection"] {
		return nil, defaultSilence
	}
	hb := ""
	deadline := defaultSilence
	if caps.Heartbeat != nil && caps.Heartbeat.Max >= caps.Heartbeat.Min && caps.Heartbeat.Max > 0 {
		sec := max(1, min(max(30, caps.Heartbeat.Min), caps.Heartbeat.Max))
		if sec <= 180 {
			hb = fmt.Sprintf("<heartbeat>%d</heartbeat>", sec)
			deadline = time.Duration(sec) * 3 * time.Second
		}
	}
	return []byte(`<SubscribeEvent version="2.0" xmlns="http://www.isapi.org/ver20/XMLSchema"><eventMode>list</eventMode><EventList><Event><type>fielddetection</type></Event><Event><type>linedetection</type></Event></EventList>` + hb + `</SubscribeEvent>`), deadline
}

func (hikvision) Receive(ctx context.Context, s Source, observe func(*CameraAlert) error) error {
	if s.Connection == nil {
		return errors.New("invalid_connection")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	client := eventClient()
	defer client.CloseIdleConnections()
	c := *s.Connection
	payload, deadline := subscription(ctx, client, c)
	var res *http.Response
	var err error
	if payload != nil {
		res, err = digestResponse(ctx, client, c, http.MethodPost, "/ISAPI/Event/notification/subscribeEvent", payload)
	}
	if payload == nil || (err != nil && err.Error() == "camera_api_unsupported") {
		res, err = digestResponse(ctx, client, c, http.MethodGet, "/ISAPI/Event/notification/alertStream", nil)
		deadline = 90 * time.Second
	}
	if err != nil {
		return err
	}
	defer res.Body.Close()
	media, params, err := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(media, "multipart/") || params["boundary"] == "" {
		return errors.New("invalid_event_stream")
	}
	stream := multipart.NewReader(res.Body, params["boundary"])
	timer := time.AfterFunc(deadline, cancel)
	defer timer.Stop()
	for {
		part, e := stream.NextPart()
		if e != nil {
			return e
		}
		partType, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if strings.HasPrefix(partType, "image/") || partType == "application/octet-stream" {
			continue
		}
		if !strings.Contains(partType, "xml") && !strings.Contains(partType, "json") {
			continue
		}
		// Some cameras include the boundary's leading CRLF in Content-Length.
		const maxEventSize = 1 << 20
		raw, e := io.ReadAll(io.LimitReader(part, maxEventSize+1))
		if e != nil {
			return e
		}
		if len(raw) > maxEventSize {
			return errors.New("invalid_event_size")
		}
		alert, valid := parseNotification(raw, strings.Contains(partType, "json"))
		if !valid {
			continue
		}
		timer.Reset(deadline)
		if err = observe(alert); err != nil {
			return err
		}
	}
}

type wireAlert struct {
	XMLName xml.Name `json:"-"`
	Type    string   `xml:"eventType" json:"eventType"`
	State   string   `xml:"eventState" json:"eventState"`
	At      string   `xml:"dateTime" json:"dateTime"`
	Name    string   `xml:"ruleName" json:"ruleName"`
	Rule    wireID   `xml:"ruleID" json:"ruleID"`
	Channel wireID   `xml:"channelID" json:"channelID"`
	Region  []struct {
		ID wireID `xml:"regionID" json:"regionID"`
	} `xml:"DetectionRegionList>DetectionRegionEntry" json:"-"`
}
type wireID string

func (v *wireID) UnmarshalJSON(b []byte) error {
	var s string
	if len(b) > 0 && b[0] == '"' {
		if e := json.Unmarshal(b, &s); e != nil {
			return e
		}
	} else {
		var n json.Number
		if e := json.Unmarshal(b, &n); e != nil {
			return e
		}
		s = n.String()
	}
	*v = wireID(s)
	return nil
}
func parseNotification(raw []byte, isJSON bool) (*CameraAlert, bool) {
	var w wireAlert
	if isJSON {
		var wrapper struct {
			Event *wireAlert `json:"EventNotificationAlert"`
		}
		if json.Unmarshal(raw, &wrapper) != nil {
			return nil, false
		}
		if wrapper.Event != nil {
			w = *wrapper.Event
		} else if json.Unmarshal(raw, &w) != nil {
			return nil, false
		}
	} else {
		if xml.Unmarshal(raw, &w) != nil || w.XMLName.Local != "EventNotificationAlert" {
			return nil, false
		}
	}
	if w.Type == "" {
		return nil, false
	}
	if w.Channel != "" && w.Channel != "1" {
		return nil, true
	}
	var kind string
	switch strings.ToLower(w.Type) {
	case "fielddetection":
		kind = "intrusion"
	case "linedetection":
		kind = "line_crossing"
	default:
		return nil, true
	}
	a := &CameraAlert{Kind: kind, RuleName: w.Name, RuleID: string(w.Rule)}
	if a.RuleID == "" && len(w.Region) == 1 {
		a.RuleID = string(w.Region[0].ID)
	}
	if len(a.RuleName) > 512 || strings.ContainsRune(a.RuleName, 0) {
		a.RuleName = ""
	}
	if len(a.RuleID) > 128 || strings.ContainsRune(a.RuleID, 0) {
		a.RuleID = ""
	}
	switch strings.ToLower(w.State) {
	case "active":
		a.State = "active"
	case "inactive":
		a.State = "inactive"
	}
	if t, e := time.Parse(time.RFC3339Nano, w.At); e == nil {
		a.CameraAt = &t
	}
	return a, true
}
