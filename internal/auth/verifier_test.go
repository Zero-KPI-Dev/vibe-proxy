package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/a448582655/vibe-proxy/internal/config"
	"golang.org/x/crypto/bcrypt"
)

func TestVerificationCacheUsesCurrentAuthorization(t *testing.T) {
	const token = "sk-verification-test"
	hash, _ := bcrypt.GenerateFromPassword([]byte(token), bcrypt.MinCost)
	keys := []config.ClientKeyConfig{{Name: "agent", KeyHash: string(hash), Enabled: true, AllowedModels: []string{"one"}, RPM: 1000}}
	a := NewAuthenticator()
	var compares atomic.Int32
	a.verifier.compare = func(hash, raw []byte) error { compares.Add(1); return bcrypt.CompareHashAndPassword(hash, raw) }
	req, _ := http.NewRequest("GET", "http://localhost/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if _, failure := a.AuthenticateDataPlane(req, keys); failure != nil {
		t.Fatal(failure)
	}
	keys[0].Name, keys[0].AllowedModels, keys[0].RPM = "renamed", []string{"two"}, 1
	client, failure := a.AuthenticateDataPlane(req, keys)
	if failure != nil || client.Name != "renamed" || ModelAllowed(client.AllowedModels, "one") || !ModelAllowed(client.AllowedModels, "two") {
		t.Fatalf("stale permissions: %+v %v", client, failure)
	}
	if compares.Load() != 1 {
		t.Fatalf("warm key re-ran bcrypt %d times", compares.Load())
	}
	if _, failure := a.AuthenticateDataPlane(req, keys); failure == nil || failure.Code != "rate_limit_exceeded" {
		t.Fatalf("cache bypassed RPM: %v", failure)
	}
	keys[0].Enabled = false
	if _, failure := a.AuthenticateDataPlane(req, keys); failure == nil || failure.Code != "invalid_api_key" {
		t.Fatalf("revocation ignored: %v", failure)
	}
	keys[0].Enabled = true
	otherHash, _ := bcrypt.GenerateFromPassword([]byte("rotated"), bcrypt.MinCost)
	keys[0].KeyHash = string(otherHash)
	if _, failure := a.AuthenticateDataPlane(req, keys); failure == nil || failure.Code != "invalid_api_key" {
		t.Fatalf("rotation ignored: %v", failure)
	}
	if _, failure := a.AuthenticateDataPlane(req, nil); failure == nil {
		t.Fatal("deleted key still accepted")
	}
}

func TestVerificationCoalescesColdBurstAndBoundsWorkers(t *testing.T) {
	v := newKeyVerifier()
	var compares atomic.Int32
	entered, release := make(chan struct{}, 8), make(chan struct{})
	defer close(release)
	v.compare = func(_, _ []byte) error { compares.Add(1); entered <- struct{}{}; <-release; return nil }
	keys := []config.ClientKeyConfig{{KeyHash: "test-hash", Enabled: true}}
	var requests sync.WaitGroup
	for i := 0; i < 32; i++ {
		requests.Add(1)
		go func() {
			defer requests.Done()
			if _, err := v.verify(context.Background(), "same", keys); err != nil {
				t.Errorf("shared verification: %v", err)
			}
		}()
	}
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	waiter := make(chan error, 1)
	go func() { _, err := v.verify(ctx, "same", keys); waiter <- err }()
	cancel()
	if err := <-waiter; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter: %v", err)
	}
	for i := 0; i < 3; i++ {
		requests.Add(1)
		go func(i int) { defer requests.Done(); _, _ = v.verify(context.Background(), fmt.Sprint(i), keys) }(i)
		<-entered
	}
	if _, err := v.verify(context.Background(), "overload", keys); !errors.Is(err, errVerificationBusy) {
		t.Fatalf("unbounded bcrypt work: %v", err)
	}
	if compares.Load() != 4 {
		t.Fatalf("duplicate bcrypt work: %d", compares.Load())
	}
	// Release all workers without closing the deferred channel twice.
	for i := 0; i < 4; i++ {
		release <- struct{}{}
	}
	requests.Wait()
}

func TestVerificationCacheIsBoundedAndFailuresAreNotCached(t *testing.T) {
	v := newKeyVerifier()
	v.compare = func(_, _ []byte) error { return nil }
	keys := []config.ClientKeyConfig{{KeyHash: "test-hash", Enabled: true}}
	for i := 0; i < verificationCacheCapacity+30; i++ {
		if _, err := v.verify(context.Background(), fmt.Sprint(i), keys); err != nil {
			t.Fatal(err)
		}
	}
	if len(v.cache) != verificationCacheCapacity || len(v.inflight) != 0 {
		t.Fatal("cache or inflight work is unbounded")
	}
	v.compare = func(_, _ []byte) error { return errors.New("invalid") }
	if index, err := v.verify(context.Background(), "invalid", keys); index != -1 || err != nil {
		t.Fatal(index, err)
	}
	for fingerprint := range v.cache {
		if fingerprint.token == sha256.Sum256([]byte("invalid")) {
			t.Fatal("negative result was cached")
		}
	}
}

func TestAuthenticationBusyIncludesRetryAfter(t *testing.T) {
	a := NewAuthenticator()
	keys := []config.ClientKeyConfig{{KeyHash: "test-only-hash", Enabled: true}}
	entered, release := make(chan struct{}, 4), make(chan struct{})
	var requests sync.WaitGroup
	defer func() { close(release); requests.Wait() }()
	a.verifier.compare = func(_, _ []byte) error {
		entered <- struct{}{}
		<-release
		return bcrypt.ErrMismatchedHashAndPassword
	}
	for i := 0; i < 4; i++ {
		requests.Add(1)
		go func(i int) {
			defer requests.Done()
			req, _ := http.NewRequest(http.MethodGet, "http://localhost/v1/models", nil)
			req.Header.Set("Authorization", fmt.Sprintf("Bearer cold-key-%d", i))
			_, _ = a.AuthenticateDataPlane(req, keys)
		}(i)
	}
	for i := 0; i < 4; i++ {
		<-entered
	}
	req, _ := http.NewRequest(http.MethodGet, "http://localhost/v1/models", nil)
	req.Header.Set("Authorization", "Bearer overloaded-key")
	_, failure := a.AuthenticateDataPlane(req, keys)
	if failure == nil || failure.StatusCode != http.StatusServiceUnavailable || failure.Code != "authentication_busy" || failure.RetryAfter != "1" {
		t.Fatalf("missing authentication overload retry hint: %+v", failure)
	}
}

func BenchmarkAuthentication(b *testing.B) {
	const token = "benchmark-only-client-key"
	hash, _ := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	keys := []config.ClientKeyConfig{{KeyHash: string(hash), Enabled: true}}
	b.Run("bcrypt_per_request", func(b *testing.B) {
		for b.Loop() {
			if err := bcrypt.CompareHashAndPassword(hash, []byte(token)); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("verified_key_cache", func(b *testing.B) {
		v := newKeyVerifier()
		_, _ = v.verify(context.Background(), token, keys)
		b.ResetTimer()
		for b.Loop() {
			if _, err := v.verify(context.Background(), token, keys); err != nil {
				b.Fatal(err)
			}
		}
	})
}
