package media_test

import (
	"bytes"
	"strings"
	"testing"

	"vms.local/backend/internal/media"
)

func TestMediaLogsRedactCredentialsAcrossWritesAndBoundIncompleteLines(t *testing.T) {
	var output bytes.Buffer
	log := media.NewRedactedLog(&output)
	for _, chunk := range []string{
		"failed to authenticate: Post \"http://127.0.0.1:19091/auth?ke",
		"y=private%!F(MISSING)secret\": connection refused\n",
		"camera rtsp://operator:private-password@192.0.2.5/stream failed\n",
		strings.Repeat("x", 80<<10),
		"rtsp://must-not-leak:end@host/private\n",
		"next ordinary diagnostic",
	} {
		if _, err := log.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, forbidden := range []string{"private", "operator:", "must-not-leak", "http://", "rtsp://"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("sensitive log content survived: %q", forbidden)
		}
	}
	if len(text) > 1024 || !strings.Contains(text, "connection refused") || !strings.Contains(text, "next ordinary diagnostic") {
		t.Fatal("redaction lost useful diagnostics or retained an oversized line")
	}
}
