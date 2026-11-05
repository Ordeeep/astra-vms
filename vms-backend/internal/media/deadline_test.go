package media

import (
	"testing"
	"time"
)

func TestLeaseKeepsAuthorityAbsoluteDeadline(t *testing.T) {
	g := NewGuard(GuardOptions{})
	if err := g.AllowSource("camera"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	lease, err := g.GrantUntil("tenant", "tab", "camera", deadline)
	if err != nil || !lease.Expires.Equal(deadline) {
		t.Fatalf("deadline changed: %v %v", lease.Expires, err)
	}
	// Waiting for the guard lock cannot extend a decision made by the core.
	g.mu.Lock()
	done := make(chan error, 1)
	expiring := time.Now().Add(50 * time.Millisecond)
	go func() { done <- g.RenewUntil(lease.Token, expiring) }()
	time.Sleep(80 * time.Millisecond)
	g.mu.Unlock()
	if err := <-done; err == nil {
		t.Fatal("expired authority was accepted after waiting")
	}
}
