package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"vms.local/backend/internal/camera"
)

// Qualification is installation evidence from a physical trial. Unknown devices
// can collect alerts immediately; a new firmware needs its own exact match.
type eventQualification struct {
	Model       string   `json:"model"`
	Firmware    string   `json:"firmware"`
	Types       []string `json:"types"`
	Supervision string   `json:"supervision"`
	Reconnect   string   `json:"reconnect"`
}

func lookupEventQualification(dir string, device camera.Device) (*eventQualification, error) {
	if dir == "" || device.Model == "" || device.Firmware == "" {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, "event-qualifications.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []eventQualification
	if err = json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	for _, q := range entries {
		if q.Model != device.Model || q.Firmware != device.Firmware {
			continue
		}
		if len(q.Types) == 0 || !(q.Supervision == "channel" || q.Supervision == "rule") || strings.TrimSpace(q.Reconnect) == "" {
			return nil, errors.New("invalid event qualification")
		}
		for _, kind := range q.Types {
			if !isAnalyticKind(kind) {
				return nil, errors.New("invalid event qualification")
			}
		}
		return &q, nil
	}
	return nil, nil
}
