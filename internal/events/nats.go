package events

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
)

type Client struct {
	conn *nats.Conn
}

func NewClient(url, certFile, keyFile, caFile string) (*Client, error) {
	opts := []nats.Option{
		nats.Name("openagentplatform-server"),
		nats.MaxReconnects(-1),
	}

	if certFile != "" && keyFile != "" {
		opts = append(opts, nats.ClientCert(certFile, keyFile))
	}
	if caFile != "" {
		opts = append(opts, nats.RootCAs(caFile))
	}

	conn, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("nats: connect: %w", err)
	}
	return &Client{conn: conn}, nil
}

func (c *Client) Conn() *nats.Conn { return c.conn }

func (c *Client) Publish(ctx context.Context, subject string, payload []byte) error {
	_ = ctx
	return c.conn.Publish(subject, payload)
}

func (c *Client) Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error) {
	return c.conn.Subscribe(subject, handler)
}

func (c *Client) Close() {
	if c.conn != nil {
		c.conn.Drain()
	}
}
