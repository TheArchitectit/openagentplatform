// Package remote provides server-side shell session management.
package remote

import (
	"github.com/nats-io/nats.go"
)

// NATSConnAdapter wraps a *nats.Conn so the API package can
// depend on the remote package without importing nats.io directly.
type NATSConnAdapter struct {
	Conn *nats.Conn
}

// NewNATSConnAdapter returns a wrapper around the given connection.
// Returns nil if conn is nil.
func NewNATSConnAdapter(conn *nats.Conn) *NATSConnAdapter {
	if conn == nil {
		return nil
	}
	return &NATSConnAdapter{Conn: conn}
}

// Subscribe implements the bridge interface.
func (a *NATSConnAdapter) Subscribe(subj string, cb nats.MsgHandler) (*nats.Subscription, error) {
	return a.Conn.Subscribe(subj, cb)
}

// Publish implements the bridge interface.
func (a *NATSConnAdapter) Publish(subj string, data []byte) error {
	return a.Conn.Publish(subj, data)
}
