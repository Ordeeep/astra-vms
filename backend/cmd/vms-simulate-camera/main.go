//go:build astra_dev

package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"vms.local/backend/internal/camerafixture"
)

func main() {
	port := flag.Int("port", 19093, "local simulator HTTP port")
	interval := flag.Duration("interval", 30*time.Second, "interval between simulated alerts")
	flag.Parse()
	if *interval < time.Second || *port < 1 || *port > 65535 {
		log.Fatal("invalid interval or port")
	}
	device := &camerafixture.Device{Interval: *interval}
	device.Line7Enabled.Store(true)
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Câmera ISAPI simulada em http://%s. Comandos: on, off (linha 7). Ctrl+C encerra a câmera.\n", listener.Addr())
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			switch strings.TrimSpace(scanner.Text()) {
			case "on":
				device.Line7Enabled.Store(true)
				fmt.Println("Linha 7 habilitada.")
			case "off":
				device.Line7Enabled.Store(false)
				fmt.Println("Linha 7 desabilitada.")
			default:
				fmt.Println("Use on ou off.")
			}
		}
	}()
	server := http.Server{Handler: device, ReadHeaderTimeout: 3 * time.Second}
	log.Fatal(server.Serve(listener))
}
