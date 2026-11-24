package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"vms.local/backend/internal/eventcollector"
	"vms.local/backend/internal/servicehost"
)

func main() {
	addr := flag.String("listen", "127.0.0.1:19092", "private collector listener")
	dir := flag.String("data", "", "private collector data directory")
	capacity := flag.Int64("queue-bytes", 1<<30, "total alert payload budget in bytes")
	reserve := flag.String("reservations", "", "JSON file mapping tenant IDs to reserved bytes; unassigned capacity is shared equally by other tenants")
	flag.Parse()
	host, _, err := net.SplitHostPort(*addr)
	if err != nil || !net.ParseIP(host).IsLoopback() || *dir == "" {
		log.Fatal("private listener and data directory required")
	}
	limits := map[string]int64{}
	if *reserve != "" {
		raw, e := os.ReadFile(*reserve)
		if e != nil || json.Unmarshal(raw, &limits) != nil {
			log.Fatal("invalid reservation file")
		}
	}
	secret := os.Getenv("VMS_WORKER_SECRET")
	store, err := eventcollector.Open(*dir, secret, *capacity, limits)
	if err != nil {
		log.Fatal("could not open private collector store")
	}
	defer store.Close()
	ctx, cancel := servicehost.SignalContext(context.Background())
	defer cancel()
	servicehost.WatchParent(cancel)
	worker, err := eventcollector.Start(ctx, store)
	if err != nil {
		log.Fatal("could not restore event collector")
	}
	defer worker.Close()
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Handler: worker.Handler(secret), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 25 * time.Second}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Print("event collector listener failed")
			cancel()
		}
	}()
	<-ctx.Done()
	closeCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	srv.Shutdown(closeCtx)
}
