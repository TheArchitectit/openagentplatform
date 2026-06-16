//go:build unix

package checkers

import (
	"context"
	"net"
	"os"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// PingICMP sends a single ICMP echo request on Unix-like systems.
func PingICMP(ctx context.Context, target string, timeout time.Duration) (*Result, error) {
	start := time.Now()
	dst, err := net.ResolveIPAddr("ip4", target)
	if err != nil {
		return nil, err
	}
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		// Fall back to datagram socket (no raw socket required) if permitted
		conn, err = icmp.ListenPacket("udp4", "0.0.0.0")
		if err != nil {
			return &Result{OK: false, Error: "icmp listen: " + err.Error(), Duration: time.Since(start).Milliseconds()}, nil
		}
	}
	defer conn.Close()

	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho, Code: 0,
		Body: &icmp.Echo{ID: os.Getpid() & 0xffff, Seq: 1, Data: []byte("oap-ping")},
	}
	bin, err := msg.Marshal(nil)
	if err != nil {
		return nil, err
	}
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	if _, err := conn.WriteTo(bin, dst); err != nil {
		return nil, err
	}

	reply := make([]byte, 1500)
	for {
		if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			return nil, err
		}
		n, peer, err := conn.ReadFrom(reply)
		if err != nil {
			return &Result{OK: false, Error: "icmp read: " + err.Error(), Duration: time.Since(start).Milliseconds()}, nil
		}
		rm, err := icmp.ParseMessage(1 /* ICMP for IPv4 */, reply[:n])
		if err != nil {
			continue
		}
		if rm.Type == ipv4.ICMPTypeEchoReply {
			return &Result{
				OK:       true,
				Status:   "reply",
				Message:  "reply from " + peer.String(),
				Value:    map[string]interface{}{"rtt_ms": time.Since(start).Milliseconds()},
				Duration: time.Since(start).Milliseconds(),
			}, nil
		}
		_ = ctx
	}
}
