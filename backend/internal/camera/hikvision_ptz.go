package camera

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strings"
	"time"
)

type hikvisionPTZ struct{ source Source }

func (hikvision) OpenPTZ(s Source) (PTZDriver, error) { return &hikvisionPTZ{source: s}, nil }

func (d *hikvisionPTZ) request(ctx context.Context, method, path string, payload []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: time.Second}).DialContext, ResponseHeaderTimeout: time.Second, MaxResponseHeaderBytes: 64 << 10}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 1500 * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return digestRequest(ctx, client, *d.source.Connection, method, "/ISAPI/PTZCtrl/channels/1/"+path, payload)
}

func (d *hikvisionPTZ) Capabilities(ctx context.Context) (PTZCapabilities, error) {
	raw, err := d.request(ctx, "GET", "capabilities", nil)
	if err != nil {
		return PTZCapabilities{}, err
	}
	var data struct {
		XMLName      xml.Name  `xml:"PTZChanelCap"`
		PanTilt      *struct{} `xml:"ContinuousPanTiltSpace"`
		Zoom         *struct{} `xml:"ContinuousZoomSpace"`
		Home         bool      `xml:"homePostionSupport"`
		NoContinuous bool      `xml:"notSupportPTZContinuous"`
	}
	if err = hikvisionXML(raw, &data); err != nil {
		return PTZCapabilities{}, err
	}
	return PTZCapabilities{PanTilt: data.PanTilt != nil && !data.NoContinuous, Zoom: data.Zoom != nil && !data.NoContinuous, Home: data.Home && !data.NoContinuous && (data.PanTilt != nil || data.Zoom != nil)}, nil
}

func (d *hikvisionPTZ) write(ctx context.Context, path string, payload []byte) error {
	raw, err := d.request(ctx, "PUT", path, payload)
	if err != nil {
		return err
	}
	var status struct {
		XMLName xml.Name `xml:"ResponseStatus"`
		Code    *int     `xml:"statusCode"`
		Sub     string   `xml:"subStatusCode"`
	}
	// Decode strictly, including trailing data; unlike discovery, a successful
	// ResponseStatus is the expected body for a device write.
	dec := xml.NewDecoder(strings.NewReader(string(raw)))
	if dec.Decode(&status) != nil {
		return errors.New("camera_api_response_invalid")
	}
	for {
		token, e := dec.Token()
		if e != nil {
			if e == io.EOF {
				break
			}
			return errors.New("camera_api_response_invalid")
		}
		if text, ok := token.(xml.CharData); !ok || strings.TrimSpace(string(text)) != "" {
			return errors.New("camera_api_response_invalid")
		}
	}
	if status.Code == nil {
		return errors.New("camera_api_response_invalid")
	}
	if *status.Code == 0 || *status.Code == 1 {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(status.Sub)) {
	case "notauthorized", "nopermission", "unauthorized", "badusernameorpassword":
		return errors.New("camera_auth_failed")
	case "devicebusy":
		return errors.New("ptz_device_busy")
	default:
		return errors.New("camera_api_unsupported")
	}
}

func (d *hikvisionPTZ) continuous(ctx context.Context, m PTZMove) error {
	payload := fmt.Sprintf(`<PTZData version="2.0" xmlns="http://www.isapi.org/ver20/XMLSchema"><pan>%d</pan><tilt>%d</tilt><zoom>%d</zoom></PTZData>`, int(math.Round(m.Pan*100)), int(math.Round(m.Tilt*100)), int(math.Round(m.Zoom*100)))
	return d.write(ctx, "continuous", []byte(payload))
}
func (d *hikvisionPTZ) Move(ctx context.Context, m PTZMove) error {
	if !m.Valid() {
		return errors.New("invalid_ptz_command")
	}
	return d.continuous(ctx, m)
}
func (d *hikvisionPTZ) Stop(ctx context.Context) error { return d.continuous(ctx, PTZMove{}) }

// ISAPI PTZ Service v2.0 §4.7 defines no query or request body. Do not
// synthesize an undocumented motionDirection or overwrite homeposition.
func (d *hikvisionPTZ) Home(ctx context.Context) error { return d.write(ctx, "homeposition/goto", nil) }
