//go:build astra_dev

package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"vms.local/backend/internal/app"
)

func main() {
	tenant := flag.String("tenant", "", "development tenant ID")
	camera := flag.String("camera", "", "camera number in the tenant")
	flag.Parse()
	databaseURL := os.Getenv("VMS_DATABASE_URL")
	if databaseURL == "" || *tenant == "" || *camera == "" {
		log.Fatal("VMS_DATABASE_URL, -tenant and -camera are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	count, err := app.SimulateAnalyticEvents(ctx, databaseURL, *tenant, *camera)
	if err != nil {
		log.Fatal("could not record simulated events; verify the development database, tenant and camera")
	}
	log.Printf("recorded %d simulated events", count)
}
