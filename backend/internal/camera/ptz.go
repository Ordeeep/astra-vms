package camera

import (
	"context"
	"errors"
	"math"
)

// Physical resources, deliberately separate from the operator's permissions.
type PTZCapabilities struct {
	PanTilt bool `json:"panTilt"`
	Zoom    bool `json:"zoom"`
	Home    bool `json:"home"`
}

// Normalized signed velocities; vendor units never escape the adapter.
type PTZMove struct {
	Pan  float64 `json:"pan"`
	Tilt float64 `json:"tilt"`
	Zoom float64 `json:"zoom"`
}

func (m PTZMove) Valid() bool {
	for _, v := range []float64{m.Pan, m.Tilt, m.Zoom} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < -1 || v > 1 {
			return false
		}
	}
	return m != (PTZMove{})
}

type PTZDriver interface {
	Capabilities(context.Context) (PTZCapabilities, error)
	Move(context.Context, PTZMove) error
	Stop(context.Context) error
	Home(context.Context) error
}

// PTZ is optional: adding it to one adapter does not require fake methods on
// RTSP or vendors which have not implemented device control yet.
type PTZAdapter interface {
	OpenPTZ(Source) (PTZDriver, error)
}

func OpenPTZ(s Source) (PTZDriver, error) {
	a, ok := registry[s.Type].(PTZAdapter)
	if !ok {
		return nil, errors.New("ptz_unsupported")
	}
	if err := validateManagedSource(s); err != nil {
		return nil, err
	}
	return a.OpenPTZ(s)
}
