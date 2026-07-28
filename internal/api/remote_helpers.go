package api

import (
	"encoding/json"

	"github.com/nats-io/nats.go"
)

// natsMsg is a type alias for nats.Msg so the helpers stay
// readable and tests can substitute fakes.
type natsMsg = nats.Msg

// decodeNATSMsg unmarshals the message data into v.
func decodeNATSMsg(m *natsMsg, v any) error {
	return json.Unmarshal(m.Data, v)
}
