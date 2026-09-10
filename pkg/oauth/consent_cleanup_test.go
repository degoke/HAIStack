package oauth

import (
	"context"
	"net/url"
	"testing"
	"time"
)

func TestRunConsentSessionCleanup(t *testing.T) {
	now := time.Now()
	store := NewMemoryConsentSessionStore(func() time.Time { return now })
	store.sessions["expired"] = consentSession{
		Issuer:    "https://example.com",
		ExpiresAt: now.Add(-time.Minute),
	}
	store.sessions["active"] = consentSession{
		Issuer:    "https://example.com",
		ExpiresAt: now.Add(time.Minute),
	}

	ctx, cancel := context.WithCancel(context.Background())
	go RunConsentSessionCleanup(ctx, store, 25*time.Millisecond)
	time.Sleep(75 * time.Millisecond)
	cancel()

	if _, ok := store.sessions["expired"]; ok {
		t.Fatal("expected expired session to be purged by background cleanup")
	}
	if _, ok := store.sessions["active"]; !ok {
		t.Fatal("expected active session to remain")
	}
}

func TestMemoryConsentSessionStorePurgeExpired(t *testing.T) {
	now := time.Now()
	store := NewMemoryConsentSessionStore(func() time.Time { return now })
	store.sessions["old"] = consentSession{
		Issuer:    "https://example.com",
		Params:    url.Values{},
		ExpiresAt: now.Add(-time.Second),
	}
	removed, err := store.PurgeExpired(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d", removed)
	}
}
