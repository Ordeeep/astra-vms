package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"vms.local/backend/internal/app"
)

func TestCoreRestartPreservesCredentialsCameraIdentityAndEncryptionKey(t *testing.T) {
	b := newTestAPI(t)
	user := b.login("admin", "admin")
	tenant := b.call("GET", "/me", nil, 200)["tenants"].([]any)[0].(map[string]any)["id"].(string)
	b.selectTenant(tenant)
	base := "/tenants/" + tenant
	site := b.call("POST", base+"/sites", map[string]any{"name": "Restart", "timezone": "UTC"}, 201)["id"].(string)
	camera := b.call("POST", base+"/cameras", map[string]any{"name": "Preserved", "siteId": site, "profiles": []any{map[string]string{"id": "main", "name": "Main", "url": "rtsp://user:camera-secret@127.0.0.1/camera"}}}, 201)
	b.call("POST", "/auth/password", map[string]any{"currentPassword": "admin", "newPassword": "changed-before-update"}, 204)
	b.close()
	restarted := newTestAPI(t, func(o *app.Options) { *o = b.options })
	if restarted.login("admin", "changed-before-update") != user {
		t.Fatal("bootstrap replaced the existing account")
	}
	restarted.selectTenant(tenant)
	restored := restarted.call("GET", base+"/cameras/"+camera["id"].(string), nil, 200)
	if restored["sourceVersion"] != camera["sourceVersion"] || restored["siteId"] != site {
		t.Fatal("restart replaced camera provenance")
	}
	restarted.close()
	path := filepath.Join(b.options.DataDir, "camera-key.bin")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, make([]byte, len(original)), 0600); err != nil {
		t.Fatal(err)
	}
	defer os.WriteFile(path, original, 0600)
	failed, err := app.New(context.Background(), b.options)
	if err == nil {
		failed.Close()
		t.Fatal("database reopened with another installation's camera key")
	}
	if !strings.Contains(err.Error(), "encryption key") {
		t.Fatalf("wrong failure: %v", err)
	}
}
