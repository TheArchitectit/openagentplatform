package api

import (
	"github.com/nats-io/nats.go"
)

// natsConnAdapter adapts *nats.Conn to the NATSConn interface used by
// the shell bridge. It lives in this package because NATSConn's
// Subscribe signature uses the unexported natsMsgHandler named type,
// which outside packages cannot spell.
type natsConnAdapter struct{ conn *nats.Conn }

func (a *natsConnAdapter) Subscribe(subj string, cb natsMsgHandler) (NATSSub, error) {
	return a.conn.Subscribe(subj, nats.MsgHandler(cb))
}

func (a *natsConnAdapter) Publish(subj string, data []byte) error {
	return a.conn.Publish(subj, data)
}

// NewShellNATSConn returns a NATSConn backed by the given connection.
// Returns nil when conn is nil (dev/test mode without NATS).
func NewShellNATSConn(conn *nats.Conn) NATSConn {
	if conn == nil {
		return nil
	}
	return &natsConnAdapter{conn: conn}
}
