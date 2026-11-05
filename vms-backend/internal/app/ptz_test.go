package app_test

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
	"vms.local/backend/internal/app"
)

func TestPTZAuthorizationIsolationAndRevocation(t *testing.T) {
	var writes atomic.Int32
	device := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ISAPI/Streaming/channels":
			fmt.Fprint(w, `<StreamingChannelList><StreamingChannel><id>101</id><Video><videoCodecType>H.264</videoCodecType></Video></StreamingChannel></StreamingChannelList>`)
		case "/ISAPI/Security/adminAccesses":
			fmt.Fprint(w, `<AdminAccessProtocolList><AdminAccessProtocol><protocol>RTSP</protocol><portNo>554</portNo></AdminAccessProtocol></AdminAccessProtocolList>`)
		case "/ISAPI/PTZCtrl/channels/1/capabilities":
			fmt.Fprint(w, `<PTZChanelCap><ContinuousPanTiltSpace/><ContinuousZoomSpace/><homePostionSupport>true</homePostionSupport></PTZChanelCap>`)
		case "/ISAPI/PTZCtrl/channels/1/continuous":
			writes.Add(1)
			fmt.Fprint(w, `<ResponseStatus><statusCode>1</statusCode></ResponseStatus>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer device.Close()
	b := newTestAPI(t)
	admin := b.login("admin", "admin")
	tenant := b.call("POST", "/admin/tenants", map[string]any{"name": "PTZ laboratory"}, 201)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	site := b.call("POST", base+"/sites", map[string]any{"name": "PTZ lab", "timezone": "UTC"}, 201)["id"].(string)
	u, _ := url.Parse(device.URL)
	port, _ := strconv.Atoi(u.Port())
	input := map[string]any{"name": "PTZ fixture", "siteId": site, "connectionType": "hikvision", "connection": map[string]any{"host": u.Hostname(), "protocol": "http", "httpPort": port, "username": "fixture", "password": "fixture-password"}}
	cid := b.call("POST", base+"/cameras", input, 201)["id"].(string)
	path := base + "/cameras/" + cid + "/ptz"
	move := map[string]any{"pan": .6, "tilt": .6, "zoom": 0}
	// Global administration is not physical device authorization.
	b.call("GET", path, nil, 403)
	b.call("POST", path+"/commands", move, 403)
	groupInput := map[string]any{"name": "PTZ operator", "grants": []app.Grant{{Action: "live", Scope: "site", ResourceID: site}, {Action: "ptz.control", Scope: "site", ResourceID: site}}}
	group := b.call("POST", base+"/groups", groupInput, 201)["id"].(string)
	b.call("PUT", base+"/operators/"+admin, map[string]any{"groups": []string{group}, "revision": 0}, 200)
	operator := b.call("POST", "/admin/operators", map[string]any{"username": "ptz-second", "password": "ptz-test-password"}, 201)["id"].(string)
	b.call("PUT", base+"/operators/"+operator, map[string]any{"groups": []string{group}, "revision": 0}, 200)
	peerClient := &http.Client{Transport: b.client.Transport}
	peerClient.Jar, _ = cookiejar.New(nil)
	peer := &browserClient{t: t, client: peerClient, server: b.server}
	peer.login("ptz-second", "ptz-test-password")
	peer.selectTenant(tenant)
	caps := b.call("GET", path, nil, 200)
	if caps["panTilt"] != true || caps["zoom"] != true {
		t.Fatal(caps)
	}
	move["revision"] = caps["revision"]
	first := b.call("POST", path+"/commands", move, 201)["id"].(string)
	second := peer.call("POST", path+"/commands", map[string]any{"zoom": -.4, "revision": caps["revision"]}, 201)["id"].(string)
	before := writes.Load()
	b.call("DELETE", path+"/commands/"+first, nil, 204)
	b.call("PUT", path+"/commands/"+first, nil, 409)
	if writes.Load() != before {
		t.Fatal("old command interrupted successor")
	}
	b.call("DELETE", path+"/commands/"+second, nil, 204)
	if writes.Load() != before {
		t.Fatal("another owner stopped the new command")
	}
	peer.call("PUT", path+"/commands/"+second, nil, 200)
	b.call("POST", path+"/commands", map[string]any{"pan": 2}, 422)
	b.call("POST", base+"/cameras/foreign/ptz/commands", move, 404)
	other := b.call("POST", "/admin/tenants", map[string]any{"name": "Other PTZ tenant"}, 201)["id"].(string)
	b.call("POST", "/tenants/"+other+"/cameras/"+cid+"/ptz/commands", move, 404)
	if writes.Load() != before {
		t.Fatal("unauthorized device command")
	}
	abandoned := peer.call("POST", path+"/commands", move, 201)["id"].(string)
	watchBefore := writes.Load()
	watchDeadline := time.Now().Add(4 * time.Second)
	for writes.Load() == watchBefore && time.Now().Before(watchDeadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if writes.Load() != watchBefore+1 {
		t.Fatal("HTTP command without renewals was not stopped by watchdog")
	}
	peer.call("PUT", path+"/commands/"+abandoned, nil, 409)
	second = peer.call("POST", path+"/commands", move, 201)["id"].(string)
	before = writes.Load()
	groupInput["revision"] = 1
	groupInput["grants"] = []app.Grant{{Action: "live", Scope: "site", ResourceID: site}}
	b.call("PUT", base+"/groups/"+group, groupInput, 200)
	peer.call("PUT", path+"/commands/"+second, nil, 403)
	b.call("POST", path+"/commands", move, 403)
	// Revocation cannot prevent the original owner from safely stopping its
	// existing command; absent a browser, the watchdog performs that cleanup.
	b.call("DELETE", path+"/commands/"+second, nil, 204)
	deadline := time.Now().Add(4 * time.Second)
	for writes.Load() == before && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if writes.Load() <= before {
		t.Fatal("revoked/lost control was not stopped")
	}
	groupInput["revision"] = 2
	groupInput["grants"] = []app.Grant{{Action: "ptz.control", Scope: "site", ResourceID: site}}
	b.call("PUT", base+"/groups/"+group, groupInput, 200)
	b.call("POST", path+"/commands", move, 403)
	visible := b.call("GET", base+"/cameras/"+cid, nil, 200)
	if visible["capabilities"].(map[string]any)["ptz.control"] != false {
		t.Fatal("PTZ without live was exposed as authorized")
	}
	otherSite := b.call("POST", base+"/sites", map[string]any{"name": "Other scope", "timezone": "UTC"}, 201)["id"].(string)
	groupInput["revision"] = 3
	groupInput["grants"] = []app.Grant{{Action: "live", Scope: "site", ResourceID: site}, {Action: "ptz.control", Scope: "site", ResourceID: otherSite}}
	b.call("PUT", base+"/groups/"+group, groupInput, 200)
	b.call("POST", path+"/commands", move, 403)
}
