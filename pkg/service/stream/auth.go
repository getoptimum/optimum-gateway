package stream

import (
	"fmt"

	"github.com/getoptimum/optimum-gateway/pkg/service/auth_token"
)

// ConsumerAuthenticator validates a consumer JWT and returns the subject used
// for per-connection caps (ADR-0011). Implementations are selected by
// stream_require_auth at wiring time.
type ConsumerAuthenticator interface {
	Authenticate(token string) (subject string, err error)
}

const devLocalSubject = "dev-local"

// NewConsumerAuthenticator returns the auth backend for the consumer stream.
func NewConsumerAuthenticator(auth *auth_token.Service, requireAuth bool) ConsumerAuthenticator {
	if !requireAuth {
		return allowAllAuthenticator{}
	}
	return jwksAuthenticator{auth: auth}
}

type jwksAuthenticator struct {
	auth *auth_token.Service
}

func (a jwksAuthenticator) Authenticate(token string) (string, error) {
	claims, err := a.auth.VerifyStreamToken(token)
	if err != nil {
		return "", err
	}
	if claims == nil {
		return "", fmt.Errorf("stream auth: verifier unavailable")
	}
	return claims.Subject, nil
}

type allowAllAuthenticator struct{}

func (allowAllAuthenticator) Authenticate(string) (string, error) {
	return devLocalSubject, nil
}
