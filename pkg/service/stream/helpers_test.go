package stream

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
)

// revocableAuth authenticates until revoked, which is what expiry looks like
// to a handler without depending on the verifier's 30s clock skew.
type revocableAuth struct{ revoked atomic.Bool }

func (a *revocableAuth) Authenticate(string) (string, error) {
	if a.revoked.Load() {
		return "", errors.New("token no longer valid")
	}
	return testSubject, nil
}

// testSubject is the consumer identity the stub authenticators return.
const testSubject = "sub-1"

// subjectAuth maps opaque tokens to subjects and can revoke them individually,
// which is what lets a test observe which token a connection actually holds.
type subjectAuth struct {
	mu       sync.Mutex
	subjects map[string]string
	revoked  map[string]bool
	calls    map[string]int
}

func newSubjectAuth(subjects map[string]string) *subjectAuth {
	return &subjectAuth{subjects: subjects, revoked: map[string]bool{}, calls: map[string]int{}}
}

func (a *subjectAuth) Authenticate(token string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls[token]++
	if a.revoked[token] {
		return "", errors.New("token revoked")
	}
	subject, ok := a.subjects[token]
	if !ok {
		return "", errors.New("unknown token")
	}
	return subject, nil
}

func (a *subjectAuth) revoke(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.revoked[token] = true
}

// bearerCtx carries an arbitrary bearer string, for the stubs above that do not
// need a real signed JWT.
func bearerCtx(t *testing.T, token string) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}

// authenticated reports how often a token was presented, the only sound
// evidence a send loop consumed a refresh.
func (a *subjectAuth) authenticated(token string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls[token]
}
