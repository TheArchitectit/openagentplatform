package api

import (
	"encoding/json"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/nats-io/nats.go"
)

// natsMsg is a type alias for nats.Msg so the helpers stay
// readable and tests can substitute fakes.
type natsMsg = nats.Msg

// decodeNATSMsg unmarshals the message data into v.
func decodeNATSMsg(m *natsMsg, v any) error {
	return json.Unmarshal(m.Data, v)
}

// devClaims builds a minimal RegisteredClaims with the given subject
// used for the insecure dev fallback in verifyWSUser.
func devClaims(sub string) jwt.RegisteredClaims {
	now := time.Now()
	return jwt.RegisteredClaims{
		Subject:   sub,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
	}
}
