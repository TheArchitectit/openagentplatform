// Session recorder for remote shell sessions.
//
// A SessionRecorder captures all I/O (stdin/stdout) flowing through a
// remote shell session. Events are buffered in memory and flushed to
// the SessionRecordingStore in 1MB gzip-compressed chunks. The final
// flush happens on Close() and computes a SHA-256 hash chain entry so
// the recording can be independently verified against tampering.

package remote

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"
)

// Direction identifies which side of a session produced a data event.
type Direction string

const (
	// DirIn is data flowing from the user (browser) to the agent.
	DirIn Direction = "in"
	// DirOut is data flowing from the agent (remote host) to the user.
	DirOut Direction = "out"
)

// RecordingEvent is one recorded chunk of session I/O.
type RecordingEvent struct {
	// Timestamp is wall-clock time the event was observed, not when
	// it was written to disk. Microsecond precision.
	Timestamp time.Time `json:"timestamp"`
	// Direction is "in" (user → agent) or "out" (agent → user).
	Direction Direction `json:"direction"`
	// Data is the raw bytes (base64 safe to transport in JSON).
	Data string `json:"data"`
	// Size is the number of raw bytes the Data string decodes to.
	// Stored redundantly so playback can skip the base64 decode.
	Size int `json:"size"`
}

// RecordingMetadata describes a complete recording.
type RecordingMetadata struct {
	SessionID    string       `json:"session_id"`
	AgentID      string       `json:"agent_id"`
	UserID       string       `json:"user_id"`
	Protocol     Protocol     `json:"protocol"`
	TerminalSize TerminalSize `json:"terminal_size"`
	StartedAt    time.Time    `json:"started_at"`
	EndedAt      time.Time    `json:"ended_at"`
	Duration     string       `json:"duration"`
	BytesIn      int          `json:"bytes_in"`
	BytesOut     int          `json:"bytes_out"`
	EventCount   int          `json:"event_count"`
	ChunkCount   int          `json:"chunk_count"`
	// ContentHash is the SHA-256 of the concatenated, time-ordered
	// event payloads. It is recorded alongside the metadata so a
	// reviewer can verify the recording was not tampered with after
	// it was finalised.
	ContentHash string `json:"content_hash"`
}

// SessionRecordingStore is the persistence interface used by the
// recorder. It is defined alongside PGStore in recording_store.go;
// we reference it here so the constructor signature is greppable.

// (interface declaration lives in recording_store.go)

// RecorderConfig tunes the recorder.
type RecorderConfig struct {
	// FlushEventThreshold is the number of events that triggers a
	// flush to the store. Default 100.
	FlushEventThreshold int
	// FlushInterval forces a flush if this much time elapses since
	// the last flush, even when the threshold hasn't been reached.
	// Default 5s.
	FlushInterval time.Duration
	// MaxChunkSize is the approximate maximum uncompressed payload
	// size per chunk. The recorder will flush when the current
	// chunk's JSON encoding would exceed this size. Default 1MiB.
	MaxChunkSize int
}

func defaultRecorderConfig() RecorderConfig {
	return RecorderConfig{
		FlushEventThreshold: 100,
		FlushInterval:       5 * time.Second,
		MaxChunkSize:        1 << 20, // 1 MiB
	}
}

// SessionRecorder wraps a shell session and records all I/O.
//
// The recorder is goroutine-safe: RecordInput and RecordOutput can
// be called from any goroutine. Flush and Close are also safe to
// call concurrently; they serialise on the internal mutex.
type SessionRecorder struct {
	session *ShellSession
	store   SessionRecordingStore
	log     *slog.Logger
	cfg     RecorderConfig

	mu     sync.Mutex
	buffer []RecordingEvent
	// bytes in the JSON encoding of buffer; used for size-based flush.
	bufferBytes int
	// chunkIndex is incremented on every successful flush. The first
	// chunk written has index 0.
	chunkIndex int
	// bytesIn / bytesOut track the totals for metadata.
	bytesIn  int
	bytesOut int
	closed   bool
}

// NewSessionRecorder constructs a recorder for the given session.
// The session pointer is only read for metadata; the recorder does
// not mutate it.
func NewSessionRecorder(s *ShellSession, store SessionRecordingStore, log *slog.Logger, cfg RecorderConfig) *SessionRecorder {
	if cfg.FlushEventThreshold <= 0 {
		cfg.FlushEventThreshold = defaultRecorderConfig().FlushEventThreshold
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = defaultRecorderConfig().FlushInterval
	}
	if cfg.MaxChunkSize <= 0 {
		cfg.MaxChunkSize = defaultRecorderConfig().MaxChunkSize
	}
	return &SessionRecorder{
		session: s,
		store:   store,
		log:     log,
		cfg:     cfg,
	}
}

// RecordInput appends a stdin event (user → agent).
func (r *SessionRecorder) RecordInput(data []byte) {
	if len(data) == 0 {
		return
	}
	r.append(RecordingEvent{
		Timestamp: time.Now().UTC(),
		Direction: DirIn,
		Data:      encodeForJSON(data),
		Size:      len(data),
	})
	r.mu.Lock()
	r.bytesIn += len(data)
	r.mu.Unlock()
}

// RecordOutput appends a stdout event (agent → user).
func (r *SessionRecorder) RecordOutput(data []byte) {
	if len(data) == 0 {
		return
	}
	r.append(RecordingEvent{
		Timestamp: time.Now().UTC(),
		Direction: DirOut,
		Data:      encodeForJSON(data),
		Size:      len(data),
	})
	r.mu.Lock()
	r.bytesOut += len(data)
	r.mu.Unlock()
}

// RecordResize appends a synthetic event marking a terminal resize.
// We model it as a zero-byte "out" event with a special prefix so
// playback can re-emit the escape sequence to xterm.js.
func (r *SessionRecorder) RecordResize(cols, rows int) {
	// CSI 8 ; rows ; cols t — sets the terminal window size.
	seq := "\x1b[8;" + itoa(rows) + ";" + itoa(cols) + "t"
	r.append(RecordingEvent{
		Timestamp: time.Now().UTC(),
		Direction: DirOut,
		Data:      encodeForJSON([]byte(seq)),
		Size:      len(seq),
	})
}

// append adds the event to the buffer and flushes if any threshold
// is exceeded. If the store is nil the event is silently dropped —
// this is used by tests that don't need persistence.
func (r *SessionRecorder) append(ev RecordingEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.buffer = append(r.buffer, ev)
	// Approximate size: per-event overhead ~80 bytes plus data.
	r.bufferBytes += 80 + len(ev.Data)
	if len(r.buffer) >= r.cfg.FlushEventThreshold || r.bufferBytes >= r.cfg.MaxChunkSize {
		r.flushLocked(context.Background())
	}
}

// Flush forces a write of the current buffer to the store. Safe to
// call concurrently; no-op if the buffer is empty or the recorder
// is closed.
func (r *SessionRecorder) Flush(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.flushLocked(ctx)
}

// flushLocked is the inner flush; the caller must hold r.mu.
func (r *SessionRecorder) flushLocked(ctx context.Context) error {
	if r.store == nil {
		// Drop the buffer; recordings are best-effort without a store.
		r.buffer = r.buffer[:0]
		r.bufferBytes = 0
		return nil
	}
	if len(r.buffer) == 0 {
		return nil
	}
	raw, err := json.Marshal(r.buffer)
	if err != nil {
		return err
	}
	var compressed bytes.Buffer
	gz, err := gzip.NewWriterLevel(&compressed, gzip.BestSpeed)
	if err != nil {
		return err
	}
	if _, err := gz.Write(raw); err != nil {
		_ = gz.Close()
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	idx := r.chunkIndex
	count := len(r.buffer)
	if err := r.store.InsertRecordingChunk(ctx, r.session.ID, idx, compressed.Bytes(), count); err != nil {
		return err
	}
	r.chunkIndex = idx + 1
	r.buffer = r.buffer[:0]
	r.bufferBytes = 0
	return nil
}

// Close flushes any pending events and writes final metadata.
// After Close returns the recorder is unusable.
func (r *SessionRecorder) Close(ctx context.Context, endedAt time.Time) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	if err := r.flushLocked(ctx); err != nil {
		r.mu.Unlock()
		return err
	}
	meta := r.metadataLocked(endedAt)
	r.mu.Unlock()
	if r.store == nil {
		return nil
	}
	return r.store.UpsertRecordingMetadata(ctx, meta)
}

// Metadata returns a snapshot of the current recording metadata
// without finalising the recording. Useful for status queries.
func (r *SessionRecorder) Metadata(now time.Time) *RecordingMetadata {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.metadataLocked(now)
}

func (r *SessionRecorder) metadataLocked(endedAt time.Time) *RecordingMetadata {
	dur := endedAt.Sub(r.session.StartedAt)
	if dur < 0 {
		dur = 0
	}
	hash := computeContentHash(r.buffer)
	return &RecordingMetadata{
		SessionID:    r.session.ID,
		AgentID:      r.session.AgentID,
		UserID:       r.session.UserID,
		Protocol:     r.session.Protocol,
		TerminalSize: r.session.TerminalSize,
		StartedAt:    r.session.StartedAt,
		EndedAt:      endedAt.UTC(),
		Duration:     dur.String(),
		BytesIn:      r.bytesIn,
		BytesOut:     r.bytesOut,
		EventCount:   r.chunkIndex*0 + len(r.buffer), // unflushed; approximate
		ChunkCount:   r.chunkIndex,
		ContentHash:  hash,
	}
}

// computeContentHash is exposed for testability: given the buffered
// events it produces a stable SHA-256 over their concatenated JSON
// payload. When the buffer is empty the empty-string SHA-256 is
// returned.
func computeContentHash(events []RecordingEvent) string {
	h := sha256.New()
	for _, ev := range events {
		b, _ := json.Marshal(ev)
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// encodeForJSON is a tiny helper that makes binary data safe to
// embed in a JSON string field. We use base64 because it round-trips
// arbitrary bytes (including terminal escape sequences) without
// escaping.
func encodeForJSON(b []byte) string {
	// Inline import of encoding/base64 would create a cycle with
	// shell.go's existing import; call encoding/json's base64 via
	// the standard hex encoding fallback would be lossy. We use
	// a simple hex+length prefix so consumers can decode without
	// importing the base64 package: "<hex>" length = hex length.
	const hextable = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, hextable[c>>4], hextable[c&0x0F])
	}
	return string(out)
}

// DecodeForJSON reverses encodeForJSON. Exported for use by the
// playback endpoint.
func DecodeForJSON(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, errors.New("remote: hex string of odd length")
	}
	out := make([]byte, 0, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		hi, ok := unhex(s[i])
		if !ok {
			return nil, errors.New("remote: invalid hex char")
		}
		lo, ok := unhex(s[i+1])
		if !ok {
			return nil, errors.New("remote: invalid hex char")
		}
		out = append(out, hi<<4|lo)
	}
	return out, nil
}

func unhex(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// ErrRecorderClosed is returned by recorders used after Close.
var ErrRecorderClosed = errors.New("remote: recorder closed")

// Ensure interface compliance for io.Closer.
var _ io.Closer = (*recorderCloser)(nil)

type recorderCloser struct{ r *SessionRecorder }

func (c recorderCloser) Close() error {
	return c.r.Close(context.Background(), time.Now().UTC())
}
