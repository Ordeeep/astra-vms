//go:build windows

package servicehost

import (
	"context"
	"os"
	"os/signal"
)

func SignalContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt)
}
