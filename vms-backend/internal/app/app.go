package app

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/argon2"
	"vms.local/backend/internal/ptz"
)

//go:embed schema.sql
var schemaSQL string

type Options struct {
	DatabaseURL   string
	Schema        string
	SecureCookies bool
	DataDir       string
	WorkerURL     string
	WorkerSecret  string
	WHEPURL       string
}
type App struct {
	ptz               *ptz.Controller
	availability      *availabilityMonitor
	db                *pgxpool.Pool
	owner             *pgx.Conn
	recordingMu       sync.Mutex
	quotaDirty        bool // protected by recordingMu; no capture until reconciled
	quotaDirtyTenants map[string]bool
	reductionDirty    map[string]bool
	opts              Options
	hashSlots         chan struct{}
	attemptMu         sync.Mutex
	attempts          map[string]attempt
	vault             cipher.AEAD
	probeSlots        chan struct{}
	eventSlots        chan struct{}
	background        sync.WaitGroup
	backgroundMu      sync.Mutex
	closing           bool
	closeOnce         sync.Once
	runContext        context.Context
	cancel            context.CancelFunc
}
type attempt struct {
	Count int
	Until time.Time
}
type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	GlobalAdmin bool   `json:"globalAdmin"`
	Active      bool   `json:"active"`
	Revision    int64  `json:"revision"`
}
type Tenant struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Active   bool   `json:"active"`
	Revision int64  `json:"revision"`
}
type identity struct {
	User              User
	SessionHash, CSRF string
}

func New(ctx context.Context, opts Options) (*App, error) {
	for _, endpoint := range []string{opts.WorkerURL, opts.WHEPURL} {
		if endpoint == "" {
			continue
		}
		u, e := url.Parse(endpoint)
		if e != nil || u.Scheme != "http" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
			return nil, errors.New("media control endpoint must use loopback HTTP")
		}
	}
	if opts.Schema == "" {
		opts.Schema = "vms"
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(opts.Schema) {
		return nil, errors.New("invalid database namespace")
	}
	base, err := pgx.Connect(ctx, opts.DatabaseURL)
	if err != nil {
		return nil, err
	}
	_, err = base.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{opts.Schema}.Sanitize())
	if err != nil {
		base.Close(ctx)
		return nil, err
	}
	var owns bool
	if err = base.QueryRow(ctx, "SELECT pg_try_advisory_lock(78921453,hashtext($1))", opts.Schema).Scan(&owns); err != nil || !owns {
		base.Close(ctx)
		return nil, errors.New("another core owns this installation")
	}
	ready := false
	defer func() {
		if !ready {
			base.Close(context.Background())
		}
	}()
	cfg, err := pgxpool.ParseConfig(opts.DatabaseURL)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 12
	cfg.ConnConfig.RuntimeParams["search_path"] = opts.Schema
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	runContext, cancel := context.WithCancel(ctx)
	app := &App{db: db, owner: base, opts: opts, quotaDirty: true, hashSlots: make(chan struct{}, 4), attempts: map[string]attempt{}, probeSlots: make(chan struct{}, 4), runContext: runContext, cancel: cancel}
	app.quotaDirtyTenants = map[string]bool{}
	app.reductionDirty = map[string]bool{}
	app.eventSlots = make(chan struct{}, 128)
	app.availability = newAvailabilityMonitor()
	app.ptz = ptz.New(ptzTTL)
	if err = migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	var hasSecrets bool
	if err = db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM camera_sources v JOIN cameras c ON c.tenant_id=v.tenant_id AND c.id=v.camera_id WHERE c.deleted_at IS NULL)").Scan(&hasSecrets); err != nil {
		db.Close()
		return nil, err
	}
	if opts.DataDir != "" {
		app.vault, err = loadVault(opts.DataDir, !hasSecrets)
		if err != nil {
			db.Close()
			return nil, err
		}
	} else if hasSecrets {
		db.Close()
		return nil, errors.New("installation data directory required to restore camera credentials")
	}
	if err = app.bootstrap(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if hasSecrets {
		if err = app.validateVault(ctx); err != nil {
			db.Close()
			return nil, errors.New("camera encryption key does not match this installation")
		}
	}
	app.startBackground(app.ptzLoop)
	app.startBackground(app.cleanTransientSessions)
	app.startBackground(app.monitorOwnership)
	app.startBackground(app.dispatchEvents)
	if opts.WorkerURL != "" {
		app.startBackground(app.availabilityLoop)
		app.startBackground(app.recordingLoop)
		app.startBackground(app.archiveMaintenance)
		app.startBackground(app.exportLoop)
		app.startBackground(app.cameraDeletionLoop)
		app.startBackground(app.quotaLoop)
	}
	ready = true
	return app, nil
}
func (a *App) startBackground(fn func()) bool {
	a.backgroundMu.Lock()
	defer a.backgroundMu.Unlock()
	if a.closing {
		return false
	}
	a.background.Add(1)
	go func() { defer a.background.Done(); fn() }()
	return true
}
func (a *App) Close() {
	a.closeOnce.Do(func() {
		a.backgroundMu.Lock()
		a.closing = true
		a.cancel()
		a.backgroundMu.Unlock()
		a.background.Wait()
		a.ptz.Close()
		a.db.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		a.owner.Close(ctx)
	})
}
func (a *App) Done() <-chan struct{} { return a.runContext.Done() }
func (a *App) monitorOwnership() {
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-a.runContext.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(a.runContext, time.Second)
			err := a.owner.Ping(ctx)
			cancel()
			if err != nil {
				a.cancel()
				return
			}
		}
	}
}
func (a *App) cleanTransientSessions() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.runContext.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(a.runContext, 5*time.Second)
			a.db.Exec(ctx, "DELETE FROM live_sessions WHERE expires<now()-interval '30 seconds'")
			a.db.Exec(ctx, "DELETE FROM sessions WHERE expires<now()")
			a.db.Exec(ctx, "DELETE FROM playback_sessions WHERE expires<now()")
			cancel()
		}
	}
}
func newID() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func digest(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
func passwordHash(password string) string {
	var salt [16]byte
	rand.Read(salt[:])
	key := argon2.IDKey([]byte(password), salt[:], 3, 64*1024, 1, 32)
	return "argon2id$" + base64.RawStdEncoding.EncodeToString(salt[:]) + "$" + base64.RawStdEncoding.EncodeToString(key)
}
func passwordMatches(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 3 || parts[0] != "argon2id" {
		return false
	}
	salt, e1 := base64.RawStdEncoding.DecodeString(parts[1])
	key, e2 := base64.RawStdEncoding.DecodeString(parts[2])
	if e1 != nil || e2 != nil || len(salt) != 16 || len(key) != 32 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, 3, 64*1024, 1, 32)
	return subtle.ConstantTimeCompare(actual, key) == 1
}
func (a *App) bootstrap(ctx context.Context) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(78921451)"); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM operators").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		user, tenant, group := newID(), newID(), newID()
		if _, err = tx.Exec(ctx, "INSERT INTO operators(id,username,name,password_hash,global_admin) VALUES($1,'admin','admin',$2,true)", user, passwordHash("admin")); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO tenants(id,name) VALUES($1,'Ambiente padrão')", tenant); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO memberships(tenant_id,operator_id) VALUES($1,$2)", tenant, user); err != nil {
			return err
		}
		var grants []map[string]string
		for _, action := range []string{"live", "playback", "export", "inventory.manage", "permissions.manage", "mosaics.manage", "recording.manage", "audit.view"} {
			grants = append(grants, map[string]string{"action": action, "scope": "tenant", "resourceId": tenant})
		}
		raw, _ := json.Marshal(grants)
		if _, err = tx.Exec(ctx, "INSERT INTO permission_groups(id,tenant_id,name,grants) VALUES($1,$2,'Administradores iniciais',$3)", group, tenant, raw); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO group_members(tenant_id,group_id,operator_id) VALUES($1,$2,$3)", tenant, group, user); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO audit_events(actor_id,tenant_id,action,resource_id) VALUES($1,$2,'installation.bootstrap',$1)", user, tenant); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func response(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if value != nil {
		json.NewEncoder(w).Encode(value)
	}
}
func problem(w http.ResponseWriter, status int, code string) {
	response(w, status, map[string]any{"code": code, "message": code, "correlationId": newID()})
}
func body(w http.ResponseWriter, r *http.Request, value any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		problem(w, 415, "json_required")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		problem(w, 400, "invalid_request")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		problem(w, 400, "invalid_request")
		return false
	}
	return true
}
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	a.registerAccess(mux)
	a.registerInventory(mux)
	a.registerPTZ(mux)
	a.registerMedia(mux)
	a.registerMosaics(mux)
	a.registerRecording(mux)
	a.registerPlayback(mux)
	a.registerExports(mux)
	a.registerQuota(mux)
	a.registerOperations(mux)
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/events", a.protected(a.eventStream))
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if a.db.Ping(ctx) != nil {
			problem(w, 503, "database_unavailable")
			return
		}
		response(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		response(w, 200, map[string]any{"contractMajor": ContractMajor, "productVersion": "0.1.0"})
	})
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.HandleFunc("GET /api/v1/me", a.protected(a.me))
	mux.HandleFunc("PATCH /api/v1/me", a.protected(a.updateProfile))
	mux.HandleFunc("POST /api/v1/auth/logout", a.protected(a.logout))
	mux.HandleFunc("POST /api/v1/auth/password", a.protected(a.changePassword))
	mux.HandleFunc("POST /api/v1/contexts", a.protected(a.createContext))
	mux.HandleFunc("DELETE /api/v1/contexts/{context}", a.protected(a.deleteContext))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-VMS-Contract", "1")
		if a.runContext.Err() != nil {
			problem(w, 503, "service_stopping")
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Method != "GET" && r.Method != "HEAD" {
			origin := r.Header.Get("Origin")
			if origin != "" {
				u, err := url.Parse(origin)
				scheme := "http"
				if r.TLS != nil {
					scheme = "https"
				}
				if err != nil || u.Host != r.Host || u.Scheme != scheme {
					problem(w, 403, "origin_denied")
					return
				}
			}
		}
		mux.ServeHTTP(w, r)
	})
}

type authenticatedHandler func(http.ResponseWriter, *http.Request, identity)

func (a *App) protected(next authenticatedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("vms_session")
		if err != nil {
			problem(w, 401, "authentication_required")
			return
		}
		id := identity{SessionHash: digest(cookie.Value)}
		err = a.db.QueryRow(r.Context(), `SELECT u.id,u.username,u.active,u.global_admin,u.revision,u.name,u.email,s.csrf FROM sessions s JOIN operators u ON u.id=s.operator_id WHERE s.token_hash=$1 AND s.expires>now() AND u.active`, id.SessionHash).Scan(&id.User.ID, &id.User.Username, &id.User.Active, &id.User.GlobalAdmin, &id.User.Revision, &id.User.Name, &id.User.Email, &id.CSRF)
		if errors.Is(err, pgx.ErrNoRows) {
			problem(w, 401, "authentication_required")
			return
		}
		if err != nil {
			problem(w, 503, "database_unavailable")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(id.CSRF)) != 1 {
			problem(w, 403, "csrf_required")
			return
		}
		next(w, r, id)
	}
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !body(w, r, &input) {
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if input.Username == "" || len(input.Username) > 254 || len(input.Password) > 1024 {
		problem(w, 401, "invalid_credentials")
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	a.attemptMu.Lock()
	now := time.Now()
	for address, entry := range a.attempts {
		if now.After(entry.Until) {
			delete(a.attempts, address)
		}
	}
	entry := a.attempts[ip]
	if entry.Count >= 10 {
		a.attemptMu.Unlock()
		problem(w, 429, "too_many_attempts")
		return
	}
	if entry.Count == 0 {
		entry.Until = now.Add(15 * time.Minute)
	}
	entry.Count++
	a.attempts[ip] = entry
	a.attemptMu.Unlock()
	select {
	case a.hashSlots <- struct{}{}:
		defer func() { <-a.hashSlots }()
	default:
		problem(w, 503, "authentication_busy")
		return
	}
	var user User
	var hash string
	// Legacy contact emails may be duplicated or overlap another username.
	// Authenticate only an unambiguous identity; never choose an arbitrary match.
	err := a.db.QueryRow(r.Context(), `WITH candidates AS (
		SELECT id,username,active,global_admin,revision,name,email,password_hash FROM operators
		WHERE lower(username)=lower($1) OR (email<>'' AND lower(email)=lower($1))
	) SELECT * FROM candidates WHERE (SELECT count(*) FROM candidates)=1`, input.Username).Scan(&user.ID, &user.Username, &user.Active, &user.GlobalAdmin, &user.Revision, &user.Name, &user.Email, &hash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		problem(w, 503, "database_unavailable")
		return
	}
	if hash == "" {
		hash = "argon2id$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	}
	valid := passwordMatches(hash, input.Password)
	if !valid || !user.Active || user.ID == "" {
		problem(w, 401, "invalid_credentials")
		return
	}
	token, csrf := newID(), newID()
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	defer tx.Rollback(r.Context())
	var stillValid bool
	err = tx.QueryRow(r.Context(), "SELECT active AND revision=$2 AND password_hash=$3 FROM operators WHERE id=$1 FOR UPDATE", user.ID, user.Revision, hash).Scan(&stillValid)
	if err != nil || !stillValid {
		problem(w, 401, "invalid_credentials")
		return
	}
	_, err = tx.Exec(r.Context(), "INSERT INTO sessions(token_hash,operator_id,csrf,expires) VALUES($1,$2,$3,$4)", digest(token), user.ID, csrf, time.Now().Add(8*time.Hour))
	if err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	if _, err = tx.Exec(r.Context(), "INSERT INTO audit_events(actor_id,action,resource_id) VALUES($1,'operator.login',$1)", user.ID); err != nil {
		dbFailure(w)
		return
	}
	if tx.Commit(r.Context()) != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	a.attemptMu.Lock()
	delete(a.attempts, ip)
	a.attemptMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "vms_session", Value: token, Path: "/", HttpOnly: true, Secure: a.opts.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: 8 * 60 * 60})
	response(w, 200, map[string]any{"user": user, "csrfToken": csrf})
}
func (a *App) me(w http.ResponseWriter, r *http.Request, id identity) {
	rows, err := a.db.Query(r.Context(), `SELECT t.id,t.name,t.active,t.revision FROM tenants t WHERE ($1 OR EXISTS(SELECT 1 FROM memberships m WHERE m.tenant_id=t.id AND m.operator_id=$2)) ORDER BY t.name`, id.User.GlobalAdmin, id.User.ID)
	if err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	defer rows.Close()
	tenants := []Tenant{}
	for rows.Next() {
		var tenant Tenant
		if rows.Scan(&tenant.ID, &tenant.Name, &tenant.Active, &tenant.Revision) != nil {
			problem(w, 503, "database_unavailable")
			return
		}
		tenants = append(tenants, tenant)
	}
	if rows.Err() != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	response(w, 200, map[string]any{"user": id.User, "tenants": tenants, "csrfToken": id.CSRF})
}
func (a *App) logout(w http.ResponseWriter, r *http.Request, id identity) {
	if _, err := a.db.Exec(r.Context(), "WITH removed AS (DELETE FROM sessions WHERE token_hash=$1 RETURNING operator_id) INSERT INTO audit_events(actor_id,action,resource_id) SELECT operator_id,'operator.logout',operator_id FROM removed", id.SessionHash); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "vms_session", Path: "/", HttpOnly: true, Secure: a.opts.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	w.WriteHeader(204)
}
func (a *App) changePassword(w http.ResponseWriter, r *http.Request, id identity) {
	var input struct {
		Current string `json:"currentPassword"`
		New     string `json:"newPassword"`
	}
	if !body(w, r, &input) {
		return
	}
	if len(input.New) == 0 || len(input.New) > 1024 || len(input.Current) > 1024 {
		problem(w, 422, "invalid_password")
		return
	}
	select {
	case a.hashSlots <- struct{}{}:
		defer func() { <-a.hashSlots }()
	default:
		problem(w, 503, "authentication_busy")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	defer tx.Rollback(r.Context())
	var hash string
	if err = tx.QueryRow(r.Context(), "SELECT password_hash FROM operators WHERE id=$1 FOR UPDATE", id.User.ID).Scan(&hash); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	if !passwordMatches(hash, input.Current) {
		problem(w, 403, "invalid_credentials")
		return
	}
	if _, err = tx.Exec(r.Context(), "UPDATE operators SET password_hash=$1,revision=revision+1 WHERE id=$2", passwordHash(input.New), id.User.ID); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	if _, err = tx.Exec(r.Context(), "DELETE FROM sessions WHERE operator_id=$1 AND token_hash<>$2", id.User.ID, id.SessionHash); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	if _, err = tx.Exec(r.Context(), "INSERT INTO audit_events(actor_id,action,resource_id) VALUES($1,'operator.password_changed',$1)", id.User.ID); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	w.WriteHeader(204)
}
func (a *App) createContext(w http.ResponseWriter, r *http.Request, id identity) {
	var input struct {
		TenantID string `json:"tenantId"`
	}
	if !body(w, r, &input) {
		return
	}
	var exists bool
	err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM tenants t WHERE t.id=$1 AND t.active AND ($2 OR EXISTS(SELECT 1 FROM memberships m WHERE m.tenant_id=t.id AND m.operator_id=$3)))`, input.TenantID, id.User.GlobalAdmin, id.User.ID).Scan(&exists)
	if err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	if !exists {
		problem(w, 404, "tenant_not_found")
		return
	}
	contextID := newID()
	if _, err = a.db.Exec(r.Context(), "INSERT INTO view_contexts(id,session_hash,tenant_id) VALUES($1,$2,$3)", contextID, id.SessionHash, input.TenantID); err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	response(w, 201, map[string]string{"id": contextID, "tenantId": input.TenantID})
}
func (a *App) deleteContext(w http.ResponseWriter, r *http.Request, id identity) {
	_, err := a.db.Exec(r.Context(), "DELETE FROM view_contexts WHERE id=$1 AND session_hash=$2", r.PathValue("context"), id.SessionHash)
	if err != nil {
		problem(w, 503, "database_unavailable")
		return
	}
	w.WriteHeader(204)
}
func (a *App) tenantContext(w http.ResponseWriter, r *http.Request, id identity) (string, bool) {
	tenant := r.PathValue("tenant")
	if !contextValid(r.Context(), a.db, r, id) {
		problem(w, 404, "context_not_found")
		return "", false
	}
	return tenant, true
}
func contextValid(ctx context.Context, db permissionReader, r *http.Request, id identity) bool {
	tenant := r.PathValue("tenant")
	var exists bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM view_contexts c JOIN tenants t ON t.id=c.tenant_id WHERE c.id=$1 AND c.session_hash=$2 AND c.tenant_id=$3 AND t.active AND ($4 OR EXISTS(SELECT 1 FROM memberships m WHERE m.tenant_id=t.id AND m.operator_id=$5)))`, r.Header.Get("X-VMS-Context"), id.SessionHash, tenant, id.User.GlobalAdmin, id.User.ID).Scan(&exists)
	return err == nil && exists
}
func (a *App) String() string { return fmt.Sprintf("VMS API namespace=%s", a.opts.Schema) }
