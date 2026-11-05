package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"time"

	"vms.local/backend/internal/media"
	"vms.local/backend/internal/servicehost"
)

func main() {
	binary := flag.String("mediamtx", "", "absolute MediaMTX executable")
	address := flag.String("listen", "127.0.0.1:19091", "private worker listener")
	api := flag.String("media-api", "127.0.0.1:19997", "private MediaMTX API")
	whep := flag.String("whep", "127.0.0.1:18889", "private WHEP listener")
	rtsp := flag.String("rtsp", "127.0.0.1:18554", "private RTSP listener")
	udp := flag.String("udp", ":18189", "WebRTC media listener")
	host := flag.String("host", "127.0.0.1", "address reachable by browsers")
	data := flag.String("data", "", "private runtime directory")
	liveConverters := flag.Int("live-converters", 4, "maximum concurrent browser live converters (1-256)")
	flag.Parse()
	if *liveConverters < 1 || *liveConverters > 256 {
		log.Fatal("live-converters must be between 1 and 256")
	}
	secret := os.Getenv("VMS_WORKER_SECRET")
	if len(secret) < 32 || *binary == "" || *data == "" {
		log.Fatal("worker requires binary, private data directory and secret of at least 32 characters")
	}
	for _, private := range []string{*address, *api, *whep, *rtsp} {
		h, _, err := net.SplitHostPort(private)
		if err != nil || net.ParseIP(h) == nil || !net.ParseIP(h).IsLoopback() {
			log.Fatal("control listeners must be loopback")
		}
	}
	if err := os.MkdirAll(*data, 0700); err != nil {
		log.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	servicehost.WatchParent(cancel)
	var owned *media.OwnedProcess
	var ownedMu sync.Mutex
	stop := func() {
		ownedMu.Lock()
		defer ownedMu.Unlock()
		if owned != nil {
			owned.Stop()
		}
		cancel()
	}
	archive, err := media.NewArchiveService(ctx, *data)
	if err != nil {
		log.Fatal(err)
	}
	defer archive.Close()
	guard := media.NewGuard(media.GuardOptions{ControlURL: "http://" + *api, RTSPAddress: *rtsp, MaxLiveConverters: *liveConverters, ServiceSecret: secret, OnControlFailure: stop, Archive: archive})
	server := &http.Server{Addr: *address, Handler: guard, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 30 * time.Second}
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		log.Fatal(err)
	}
	go server.Serve(listener)
	config := map[string]any{"logLevel": "warn", "authMethod": "http", "authHTTPAddress": "http://" + *address + "/auth?key=" + url.QueryEscape(secret), "authHTTPExclude": []any{}, "api": true, "apiAddress": *api, "rtspAddress": *rtsp, "rtspTransports": []string{"tcp"}, "rtmp": false, "hls": false, "srt": false, "moq": false, "webrtc": true, "webrtcAddress": *whep, "webrtcLocalUDPAddress": *udp, "webrtcIPsFromInterfaces": false, "webrtcAdditionalHosts": []string{*host}, "pathDefaults": map[string]any{"record": false}, "paths": map[string]any{}}
	raw, _ := json.Marshal(config)
	configPath := filepath.Join(*data, "mediamtx.json")
	if err := os.WriteFile(configPath, raw, 0600); err != nil {
		log.Fatal(err)
	}
	cmd := exec.Command(*binary, configPath)
	stdout := media.NewRedactedLog(os.Stdout)
	stderr := media.NewRedactedLog(os.Stderr)
	defer stdout.Close()
	defer stderr.Close()
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	ownedMu.Lock()
	owned, err = media.StartOwned(cmd)
	ownedMu.Unlock()
	if err != nil {
		log.Fatal(err)
	}
	defer owned.Stop()
	go func() { cmd.Wait(); cancel() }()
	go guard.Run(ctx)
	<-ctx.Done()
	shutdown, done := context.WithTimeout(context.Background(), time.Second)
	defer done()
	server.Shutdown(shutdown)
}
