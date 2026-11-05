package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func independentSession(b *browserClient) *browserClient {
	c := *b
	c.client = &http.Client{Transport: b.client.Transport}
	c.client.Jar, _ = cookiejar.New(nil)
	c.csrf, c.view = "", ""
	return &c
}

func TestEmailLoginAndIdentifierChanges(t *testing.T) {
	b := newTestAPI(t)
	userID := b.login("admin", "admin")
	profile := map[string]any{"name": "Pedro", "email": "Pedro@example.com", "revision": 1}
	if result := b.call("PATCH", "/me", profile, 403); result["code"] != "current_password_required" {
		t.Fatal("email registration did not require password confirmation")
	}
	profile["currentPassword"] = "wrong"
	b.call("PATCH", "/me", profile, 403)
	profile["currentPassword"] = "admin"
	b.call("PATCH", "/me", profile, 200)
	login := independentSession(b)
	if login.login("  PEDRO@EXAMPLE.COM  ", "admin") != userID {
		t.Fatal("email authenticated the wrong identity")
	}
	login.call("POST", "/auth/login", map[string]any{"username": "pedro@example.com", "password": "wrong"}, 401)
	if login.login("admin", "admin") != userID {
		t.Fatal("username login stopped working")
	}
	// A name-only update needs no password. Email case is also immaterial.
	b.call("PATCH", "/me", map[string]any{"name": "Pedro Silva", "email": "PEDRO@example.com", "revision": 2}, 200)
	profile["revision"], profile["email"] = 3, "new@example.com"
	b.call("PATCH", "/me", profile, 200)
	login.call("POST", "/auth/login", map[string]any{"username": "pedro@example.com", "password": "admin"}, 401)
	if login.login("new@example.com", "admin") != userID {
		t.Fatal("new email did not become the login identifier")
	}
	// Existing usernames and emails cannot be claimed by another operator.
	b.call("POST", "/admin/operators", map[string]any{"username": "NEW@example.com", "password": "other-password"}, 409)
	b.call("POST", "/admin/operators", map[string]any{"username": "reserved@example.com", "password": "other-password"}, 201)
	profile["revision"], profile["email"] = 4, "reserved@example.com"
	b.call("PATCH", "/me", profile, 409)
	b.call("POST", "/admin/operators", map[string]any{"username": "second", "password": "second-password"}, 201)
	second := independentSession(b)
	second.login("second", "second-password")
	second.call("PATCH", "/me", map[string]any{"name": "Second", "email": "NEW@example.com", "revision": 1, "currentPassword": "second-password"}, 409)
	// Addresses longer than the old 128-byte username limit work too.
	longEmail := strings.Repeat("a", 64) + "@" + strings.Repeat("b", 60) + ".example.com"
	profile["email"] = longEmail
	b.call("PATCH", "/me", profile, 200)
	if login.login(longEmail, "admin") != userID {
		t.Fatal("long email login failed")
	}
}

func TestConcurrentEmailRegistrationCannotDuplicateIdentifier(t *testing.T) {
	b := newTestAPI(t)
	b.login("admin", "admin")
	b.call("POST", "/admin/operators", map[string]any{"username": "second", "password": "second-password"}, 201)
	second := independentSession(b)
	second.login("second", "second-password")
	start := make(chan struct{})
	results := make(chan int, 2)
	for i, session := range []*browserClient{b, second} {
		go func(i int, session *browserClient) {
			password := "admin"
			if i == 1 {
				password = "second-password"
			}
			data, _ := json.Marshal(map[string]any{"name": "Operator", "email": "shared@example.com", "revision": 1, "currentPassword": password})
			req, _ := http.NewRequest("PATCH", session.server+"/api/v1/me", bytes.NewReader(data))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-CSRF-Token", session.csrf)
			<-start
			res, err := session.client.Do(req)
			if err != nil {
				results <- 0
				return
			}
			defer res.Body.Close()
			results <- res.StatusCode
		}(i, session)
	}
	close(start)
	first, next := <-results, <-results
	if !((first == 200 && next == 409) || (first == 409 && next == 200)) {
		t.Fatalf("concurrent email claims: %d, %d", first, next)
	}
}

func TestLegacyAmbiguousEmailsNeverChooseAnAccount(t *testing.T) {
	b := newTestAPI(t)
	b.login("admin", "admin")
	b.call("POST", "/admin/operators", map[string]any{"username": "second", "password": "admin"}, 201)
	ctx := context.Background()
	db, err := pgx.Connect(ctx, b.options.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	// Contact emails were not unique before they became login identifiers.
	if _, err = db.Exec(ctx, "UPDATE "+pgx.Identifier{b.options.Schema}.Sanitize()+".operators SET email='legacy@example.com'"); err != nil {
		t.Fatal(err)
	}
	login := independentSession(b)
	login.call("POST", "/auth/login", map[string]any{"username": "legacy@example.com", "password": "admin"}, 401)
	login.login("admin", "admin")
	login.login("second", "admin")
}
