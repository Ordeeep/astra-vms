package media

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

type GuardOptions struct {
	ControlURL        string
	RTSPAddress       string
	FFmpeg            string
	MaxLiveConverters int
	ServiceSecret     string
	OnControlFailure  func()
	Archive           http.Handler
}
type Lease struct {
	Token       string    `json:"token"`
	Tenant      string    `json:"tenant"`
	Context     string    `json:"context"`
	Path        string    `json:"path"`
	Expires     time.Time `json:"expires"`
	NativeID    string    `json:"-"`
	Revoked     bool      `json:"-"`
	Established bool      `json:"-"`
}

// Guard must run outside the API process. Its bounded leases close media even
// if the API cannot send a revocation. The guard owns the media child process.
type Guard struct {
	mu                sync.Mutex
	opts              GuardOptions
	leases            map[string]*Lease
	sources           map[string]bool
	client            *http.Client
	sourceMu          sync.Mutex
	configured        map[string]string
	converters        map[string]*sourceConverter
	converted         map[string]bool
	preparations      map[string]chan struct{}
	prepareSlots      chan struct{}
	availabilitySlots chan struct{}
	runContext        context.Context
	cancel            context.CancelFunc
}

var sourcePath = regexp.MustCompile(`^[A-Za-z0-9_-]{1,160}$`)

func NewGuard(opts GuardOptions) *Guard {
	if opts.MaxLiveConverters <= 0 {
		opts.MaxLiveConverters = 4
	}
	if opts.MaxLiveConverters > 256 {
		opts.MaxLiveConverters = 256
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// The revocation batch has up to 32 concurrent calls. The default pool of
	// two idle connections would discard most sockets after every batch.
	transport.MaxIdleConnsPerHost = 32
	transport.MaxIdleConns = 64
	transport.IdleConnTimeout = 30 * time.Second
	run, cancel := context.WithCancel(context.Background())
	return &Guard{availabilitySlots: make(chan struct{}, 8), runContext: run, cancel: cancel, converters: map[string]*sourceConverter{}, converted: map[string]bool{}, preparations: map[string]chan struct{}{}, prepareSlots: make(chan struct{}, min(8, opts.MaxLiveConverters)), opts: opts, leases: map[string]*Lease{}, sources: map[string]bool{}, configured: map[string]string{}, client: &http.Client{Timeout: 300 * time.Millisecond, Transport: transport}}
}
func (g *Guard) AllowSource(path string) error {
	if !sourcePath.MatchString(path) {
		return errors.New("invalid source path")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sources[path] = true
	return nil
}
func (g *Guard) Grant(tenant, contextID, path string, ttl time.Duration) (Lease, error) {
	return g.GrantUntil(tenant, contextID, path, time.Now().Add(ttl))
}
func (g *Guard) GrantUntil(tenant, contextID, path string, expires time.Time) (Lease, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	ttl := time.Until(expires)
	if tenant == "" || contextID == "" || !g.sources[path] || ttl <= 0 || ttl > 1500*time.Millisecond {
		return Lease{}, errors.New("invalid media lease")
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return Lease{}, err
	}
	lease := Lease{Token: hex.EncodeToString(secret[:]), Tenant: tenant, Context: contextID, Path: path, Expires: expires}
	g.leases[lease.Token] = &lease
	return lease, nil
}
func (g *Guard) Renew(token string, ttl time.Duration) error {
	return g.RenewUntil(token, time.Now().Add(ttl))
}
func (g *Guard) RenewUntil(token string, expires time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	ttl := time.Until(expires)
	lease := g.leases[token]
	if ttl <= 0 || ttl > 1500*time.Millisecond || lease == nil || lease.Revoked || !time.Now().Before(lease.Expires) {
		return errors.New("media lease expired")
	}
	lease.Expires = expires
	return nil
}

// MarkEstablished is sent by the core only after MediaMTX returns a successful
// WHEP resource creation. Before that point, 404 may be a handshake race; after
// it, 404 confirms that the already-created native session no longer exists.
func (g *Guard) MarkEstablished(token string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	lease := g.leases[token]
	if lease == nil || lease.Revoked || lease.NativeID == "" || !time.Now().Before(lease.Expires) {
		return errors.New("media lease expired")
	}
	lease.Established = true
	return nil
}
func (g *Guard) Revoke(token string) {
	g.mu.Lock()
	lease := g.leases[token]
	id := ""
	if lease != nil {
		lease.Revoked = true
		id = lease.NativeID
	}
	g.mu.Unlock()
	if id != "" {
		g.kick(id)
	}
}
func (g *Guard) kick(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	g.kickContext(ctx, id)
}
func (g *Guard) kickContext(ctx context.Context, id string) {
	req, err := http.NewRequestWithContext(ctx, "POST", g.opts.ControlURL+"/v3/webrtcsessions/kick/"+url.PathEscape(id), nil)
	if err != nil {
		g.failed("invalid_control_request")
		return
	}
	req.SetBasicAuth("service", g.opts.ServiceSecret)
	resp, err := g.client.Do(req)
	if err != nil {
		g.failed("control_request_failed")
		return
	}
	// MediaMTX returns a JSON body (including on 404). Consume the bounded
	// response so repeated revocations reuse the connection instead of
	// exhausting Windows ephemeral ports.
	_, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	if readErr != nil {
		g.failed("control_response_failed")
		return
	}
	if resp.StatusCode != 200 && resp.StatusCode != 404 {
		g.failed("control_status_rejected")
		return
	}
	if resp.StatusCode == 200 || resp.StatusCode == 404 {
		g.mu.Lock()
		for _, lease := range g.leases {
			if lease.Revoked && lease.NativeID == id && (resp.StatusCode == 200 || lease.Established) {
				// Keep the revoked token until its normal cleanup deadline,
				// but a confirmed termination needs no further kick.
				lease.NativeID = ""
			}
		}
		g.mu.Unlock()
	}
	// A 404 can precede registration during a handshake. Retain that binding
	// for the existing grace period so a late native session is still cut.
}
func (g *Guard) kickBatch(ids []string) {
	if len(ids) == 0 {
		return
	}
	// One deadline covers the entire batch, including waiting for a worker.
	// On overload the owned engine is stopped, rather than retaining readers.
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	jobs := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < min(32, len(ids)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				g.kickContext(ctx, id)
			}
		}()
	}
	for _, id := range ids {
		select {
		case jobs <- id:
		case <-ctx.Done():
			g.failed("termination_budget_exceeded")
			close(jobs)
			wg.Wait()
			return
		}
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		g.failed("termination_budget_exceeded")
	}
}
func (g *Guard) failed(reason string) {
	log.Printf("media authorization control failed: %s; stopping owned engine", reason)
	if g.opts.OnControlFailure != nil {
		g.opts.OnControlFailure()
	}
}
func (g *Guard) Run(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	defer g.client.CloseIdleConnections()
	defer g.closeConverters()
	go g.runConverterCleanup(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			var expired []string
			g.mu.Lock()
			for token, lease := range g.leases {
				if lease.Revoked || !now.Before(lease.Expires) {
					lease.Revoked = true
					if lease.NativeID != "" {
						expired = append(expired, lease.NativeID)
					}
					// Retain the binding across handshake/revocation races.
					if now.Sub(lease.Expires) > 30*time.Second {
						delete(g.leases, token)
					}
				}
			}
			g.mu.Unlock()
			g.kickBatch(expired)
		}
	}
}
func equalSecret(a, b string) bool {
	return len(a) > 0 && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
func (g *Guard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == "/auth" {
		if !equalSecret(r.URL.Query().Get("key"), g.opts.ServiceSecret) {
			http.Error(w, "unauthorized", 401)
			return
		}
		var input struct{ User, Password, Token, Action, Protocol, Path, ID string }
		if r.Method != "POST" || json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&input) != nil {
			http.Error(w, "invalid", 400)
			return
		}
		service := input.User == "service" && equalSecret(input.Password, g.opts.ServiceSecret)
		if service && input.Action == "api" {
			w.WriteHeader(204)
			return
		}
		g.mu.Lock()
		allowed := service && input.Protocol == "rtsp" && g.sources[input.Path] && (input.Action == "read" || input.Action == "publish")
		if input.Action == "read" && input.Protocol == "webrtc" {
			lease := g.leases[input.Token]
			if lease != nil && !lease.Revoked && time.Now().Before(lease.Expires) && lease.Path == input.Path && input.ID != "" && (lease.NativeID == "" || lease.NativeID == input.ID) {
				lease.NativeID = input.ID
				allowed = true
			}
		}
		g.mu.Unlock()
		if !allowed {
			http.Error(w, "unauthorized", 401)
			return
		}
		w.WriteHeader(204)
		return
	}
	if !equalSecret(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), g.opts.ServiceSecret) {
		http.Error(w, "unauthorized", 401)
		return
	}
	if g.opts.Archive != nil && (strings.HasPrefix(r.URL.Path, "/archive/") || strings.HasPrefix(r.URL.Path, "/captures")) {
		g.opts.Archive.ServeHTTP(w, r)
		return
	}
	switch {
	case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/leases/") && strings.HasSuffix(r.URL.Path, "/established"):
		token := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/leases/"), "/established")
		if g.MarkEstablished(token) != nil {
			http.Error(w, "expired", 410)
			return
		}
		w.WriteHeader(204)
	case r.Method == "POST" && r.URL.Path == "/leases":
		var input struct {
			Tenant, Context, Path string
			Expires               time.Time
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil {
			http.Error(w, "invalid", 400)
			return
		}
		lease, err := g.GrantUntil(input.Tenant, input.Context, input.Path, input.Expires)
		if err != nil {
			http.Error(w, "invalid lease", 422)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lease)
	case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/leases/"):
		var input struct{ Expires time.Time }
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil || input.Expires.IsZero() {
			http.Error(w, "invalid deadline", 400)
			return
		}
		if err := g.RenewUntil(strings.TrimPrefix(r.URL.Path, "/leases/"), input.Expires); err != nil {
			http.Error(w, "expired", 410)
			return
		}
		w.WriteHeader(204)
	case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/leases/"):
		g.Revoke(strings.TrimPrefix(r.URL.Path, "/leases/"))
		w.WriteHeader(204)
	case r.Method == "POST" && r.URL.Path == "/sources":
		var input struct {
			Path, URL      string
			Compatible     bool
			ForceTranscode bool
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&input) != nil || g.SetSourceMode(r.Context(), input.Path, input.URL, input.Compatible, input.ForceTranscode) != nil {
			http.Error(w, "invalid source", 422)
			return
		}
		w.WriteHeader(204)
	case r.Method == "POST" && r.URL.Path == "/probe":
		g.serveProbe(w, r)
	case r.Method == "POST" && r.URL.Path == "/availability":
		g.availability(w, r)
	default:
		http.NotFound(w, r)
	}
}
func (g *Guard) serveProbe(w http.ResponseWriter, r *http.Request) {
	var input struct{ URL string }
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&input) != nil {
		http.Error(w, "invalid", 400)
		return
	}
	result, err := Probe(r.Context(), input.URL)
	if err != nil {
		http.Error(w, "source probe failed", 422)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// SetSource configures a native pull path. Browser callers use SetSourceMode.
func (g *Guard) SetSource(ctx context.Context, path, rtspURL string) error {
	return g.SetSourceMode(ctx, path, rtspURL, false, false)
}
