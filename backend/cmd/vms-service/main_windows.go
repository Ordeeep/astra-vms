//go:build windows

package main

import (
	"context"
	"flag"
	"golang.org/x/sys/windows/svc"
	"log"
	"os"
	"os/signal"
	"vms.local/backend/internal/servicehost"
)

type handler struct{ config servicehost.Config }

func (h handler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- servicehost.Run(ctx, h.config, nil) }()
	current := svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	status <- current
	for {
		select {
		case err := <-done:
			if err != nil {
				return true, 1
			}
			return false, 0
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				status <- current
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending, WaitHint: 12000}
				cancel()
				<-done
				return false, 0
			}
		}
	}
}
func main() {
	name := flag.String("name", "", "Windows service name")
	config := flag.String("config", "", "absolute component configuration")
	console := flag.Bool("console", false, "run under an interactive test supervisor")
	flag.Parse()
	c, err := servicehost.Load(*config)
	if err != nil {
		log.Fatal("invalid service component configuration")
	}
	if *console {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()
		if err = servicehost.Run(ctx, c, nil); err != nil {
			log.Fatal("component supervisor failed")
		}
		return
	}
	if *name == "" {
		log.Fatal("service name required")
	}
	if err = svc.Run(*name, handler{c}); err != nil {
		log.Fatal("Windows service dispatch failed")
	}
}
