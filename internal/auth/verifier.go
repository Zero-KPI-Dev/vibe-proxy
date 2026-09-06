package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"

	"github.com/a448582655/vibe-proxy/internal/config"
	"golang.org/x/crypto/bcrypt"
)

const verificationCacheCapacity = 256

var errVerificationBusy = errors.New("authentication verification is busy")

type verificationKey struct {
	token [32]byte
	keys  [32]byte
}

type verificationCall struct {
	done  chan struct{}
	index int
	err   error
}

// keyVerifier caches only successful bcrypt checks using token fingerprints,
// never plaintext tokens or authorization decisions. The ordered enabled/hash
// configuration is part of the cache key, so rotation/revocation is immediate.
// Concurrent cold requests for the same key share one expensive verification.
type keyVerifier struct {
	mu       sync.Mutex
	cache    map[verificationKey]int
	inflight map[verificationKey]*verificationCall
	workers  chan struct{}
	compare  func([]byte, []byte) error
}

func newKeyVerifier() *keyVerifier {
	return &keyVerifier{cache: make(map[verificationKey]int), inflight: make(map[verificationKey]*verificationCall), workers: make(chan struct{}, 4), compare: bcrypt.CompareHashAndPassword}
}

func (v *keyVerifier) verify(ctx context.Context, token string, keys []config.ClientKeyConfig) (int, error) {
	if err := ctx.Err(); err != nil {
		return -1, err
	}
	configuration := sha256.New()
	for _, key := range keys {
		digest := sha256.Sum256([]byte(key.KeyHash))
		configuration.Write(digest[:])
		if key.Enabled {
			configuration.Write([]byte{1})
		} else {
			configuration.Write([]byte{0})
		}
	}
	fingerprint := verificationKey{token: sha256.Sum256([]byte(token))}
	copy(fingerprint.keys[:], configuration.Sum(nil))
	v.mu.Lock()
	if index, ok := v.cache[fingerprint]; ok {
		v.mu.Unlock()
		return index, nil
	}
	if pending, ok := v.inflight[fingerprint]; ok {
		v.mu.Unlock()
		select {
		case <-ctx.Done():
			return -1, ctx.Err()
		case <-pending.done:
			// A cancelled leader must not cancel other clients sharing its key.
			if pending.err != nil && ctx.Err() == nil {
				return v.verify(ctx, token, keys)
			}
			return pending.index, pending.err
		}
	}
	select {
	case v.workers <- struct{}{}:
	default:
		v.mu.Unlock()
		return -1, errVerificationBusy
	}
	call := &verificationCall{done: make(chan struct{}), index: -1}
	v.inflight[fingerprint] = call
	v.mu.Unlock()
	for index, key := range keys {
		if call.err = ctx.Err(); call.err != nil {
			break
		}
		if key.Enabled && v.compare([]byte(key.KeyHash), []byte(token)) == nil {
			call.index = index
			break
		}
	}
	if err := ctx.Err(); err != nil {
		call.index, call.err = -1, err
	}
	v.mu.Lock()
	if call.index >= 0 && call.err == nil {
		if len(v.cache) >= verificationCacheCapacity {
			for oldest := range v.cache {
				delete(v.cache, oldest)
				break
			}
		}
		v.cache[fingerprint] = call.index
	}
	delete(v.inflight, fingerprint)
	<-v.workers
	close(call.done)
	v.mu.Unlock()
	return call.index, call.err
}
