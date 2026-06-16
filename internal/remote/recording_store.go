// PostgreSQL-backed storage for shell session recordings.
//
// A recording consists of:
//
//  1. One metadata row in session_recordings (session_id, agent_id,
//     user_id, protocol, terminal size, timestamps, byte totals, hash).
//  2. Zero or more chunk rows in session_recording_chunks, each
//     holding a gzip-compressed JSON array of RecordingEvents.
//     Chunks are numbered starting at 0 and are append-only.
//
// Both tables are created lazily by EnsureSchema; callers should
// invoke it once during startup. The store is safe for concurrent
// use; it owns no goroutines.

package remote

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionRecordingStore is the interface used by API handlers. The
// PGStore concrete type satisfies it.
type SessionRecordingStore interface {
	EnsureSchema(ctx context.Context) error

	// Chunk operations.
	InsertRecordingChunk(ctx context.Context, sessionID string, chunkIndex int, compressed []byte, eventCount int) error
	GetRecordingChunks(ctx context.Context, sessionID string) ([]RecordingChunk, error)

	// Metadata operations.
	UpsertRecordingMetadata(ctx context.Context, m *RecordingMetadata) error
	GetRecordingMetadata(ctx context.Context, sessionID string) (*RecordingMetadata, error)

	// Listing.
	ListRecordings(ctx context.Context, f RecordingListFilter) ([]RecordingMetadata, int, error)

	// Retention.
	DeleteRecording(ctx context.Context, sessionID string) error
	PurgeOlderThan(ctx context.Context, age time.Duration) (int64, error)
}

// RecordingChunk is one persisted chunk plus its decoded events.
type RecordingChunk struct {
	SessionID  string           `json:"session_id"`
	ChunkIndex int              `json:"chunk_index"`
	EventCount int              `json:"event_count"`
	Compressed []byte           `json:"-"`
	Events     []RecordingEvent `json:"events,omitempty"`
}

// RecordingListFilter narrows the result set for ListRecordings.
type RecordingListFilter struct {
	AgentID    string
	UserID     string
	SessionID  string // substring match (ILIKE %x%)
	Since      time.Time
	Until      time.Time
	Limit      int
	Offset     int
}

// PGStore is the postgres-backed SessionRecordingStore.
type PGStore struct {
	pool *pgxpool.Pool
}

// NewPGStore constructs a PGStore. The pool must not be nil.
func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

// EnsureSchema creates the recordings tables if they don't exist.
// Safe to call repeatedly.
func (s *PGStore) EnsureSchema(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return errors.New("remote: nil pg store")
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS session_recordings (
			session_id     TEXT PRIMARY KEY,
			agent_id       TEXT NOT NULL,
			user_id        TEXT NOT NULL,
			protocol       TEXT NOT NULL,
			terminal_cols  INT  NOT NULL DEFAULT 0,
			terminal_rows  INT  NOT NULL DEFAULT 0,
			started_at     TIMESTAMPTZ NOT NULL,
			ended_at       TIMESTAMPTZ,
			duration_ms    BIGINT NOT NULL DEFAULT 0,
			bytes_in       BIGINT NOT NULL DEFAULT 0,
			bytes_out      BIGINT NOT NULL DEFAULT 0,
			event_count    BIGINT NOT NULL DEFAULT 0,
			chunk_count    INT  NOT NULL DEFAULT 0,
			content_hash   TEXT NOT NULL DEFAULT '',
			created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS session_recordings_agent_idx ON session_recordings(agent_id)`,
		`CREATE INDEX IF NOT EXISTS session_recordings_user_idx ON session_recordings(user_id)`,
		`CREATE INDEX IF NOT EXISTS session_recordings_started_idx ON session_recordings(started_at DESC)`,
		`CREATE TABLE IF NOT EXISTS session_recording_chunks (
			session_id   TEXT NOT NULL REFERENCES session_recordings(session_id) ON DELETE CASCADE,
			chunk_index  INT  NOT NULL,
			event_count  INT  NOT NULL DEFAULT 0,
			compressed   BYTEA NOT NULL,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (session_id, chunk_index)
		)`,
		`CREATE INDEX IF NOT EXISTS session_recording_chunks_session_idx ON session_recording_chunks(session_id)`,
	}
	for _, q := range stmts {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("remote: ensure schema (%s): %w", firstLine(q), err)
		}
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// InsertRecordingChunk appends a single chunk row. The metadata row
// is created on-demand (with sensible defaults) if it does not yet
// exist; UpsertRecordingMetadata will fill in the real numbers.
func (s *PGStore) InsertRecordingChunk(ctx context.Context, sessionID string, chunkIndex int, compressed []byte, eventCount int) error {
	if s == nil || s.pool == nil {
		return errors.New("remote: nil pg store")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("remote: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Ensure the metadata row exists so the FK from the chunk row is
	// satisfied. We insert a minimal row; UpsertRecordingMetadata
	// will populate the real values when the session closes.
	if _, err := tx.Exec(ctx, `
		INSERT INTO session_recordings (session_id, agent_id, user_id, protocol, started_at)
		VALUES ($1, '', '', 'ssh', NOW())
		ON CONFLICT (session_id) DO NOTHING
	`, sessionID); err != nil {
		return fmt.Errorf("remote: ensure metadata: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO session_recording_chunks (session_id, chunk_index, event_count, compressed)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (session_id, chunk_index) DO UPDATE
		SET event_count = EXCLUDED.event_count,
		    compressed  = EXCLUDED.compressed
	`, sessionID, chunkIndex, eventCount, compressed); err != nil {
		return fmt.Errorf("remote: insert chunk: %w", err)
	}
	return tx.Commit(ctx)
}

// UpsertRecordingMetadata creates or updates the metadata row.
func (s *PGStore) UpsertRecordingMetadata(ctx context.Context, m *RecordingMetadata) error {
	if s == nil || s.pool == nil {
		return errors.New("remote: nil pg store")
	}
	if m == nil || m.SessionID == "" {
		return errors.New("remote: metadata missing session_id")
	}
	dur, _ := time.ParseDuration(m.Duration)
	durMS := dur.Milliseconds()
	if dur < 0 {
		durMS = 0
	}
	ended := m.EndedAt
	if ended.IsZero() {
		ended = time.Now().UTC()
	}
	const q = `
		INSERT INTO session_recordings (
			session_id, agent_id, user_id, protocol,
			terminal_cols, terminal_rows,
			started_at, ended_at, duration_ms,
			bytes_in, bytes_out, event_count, chunk_count, content_hash
		) VALUES (
			$1,$2,$3,$4,
			$5,$6,
			$7,$8,$9,
			$10,$11,$12,$13,$14
		)
		ON CONFLICT (session_id) DO UPDATE SET
			agent_id      = EXCLUDED.agent_id,
			user_id       = EXCLUDED.user_id,
			protocol      = EXCLUDED.protocol,
			terminal_cols = EXCLUDED.terminal_cols,
			terminal_rows = EXCLUDED.terminal_rows,
			started_at    = EXCLUDED.started_at,
			ended_at      = EXCLUDED.ended_at,
			duration_ms   = EXCLUDED.duration_ms,
			bytes_in      = EXCLUDED.bytes_in,
			bytes_out     = EXCLUDED.bytes_out,
			event_count   = EXCLUDED.event_count,
			chunk_count   = EXCLUDED.chunk_count,
			content_hash  = EXCLUDED.content_hash
	`
	_, err := s.pool.Exec(ctx, q,
		m.SessionID, m.AgentID, m.UserID, string(m.Protocol),
		m.TerminalSize.Cols, m.TerminalSize.Rows,
		m.StartedAt, ended, durMS,
		m.BytesIn, m.BytesOut, m.EventCount, m.ChunkCount, m.ContentHash,
	)
	if err != nil {
		return fmt.Errorf("remote: upsert metadata: %w", err)
	}
	return nil
}

// GetRecordingMetadata returns one metadata row.
func (s *PGStore) GetRecordingMetadata(ctx context.Context, sessionID string) (*RecordingMetadata, error) {
	if s == nil || s.pool == nil {
		return nil, errors.New("remote: nil pg store")
	}
	const q = `
		SELECT session_id, agent_id, user_id, protocol,
		       terminal_cols, terminal_rows,
		       started_at, COALESCE(ended_at, started_at),
		       duration_ms, bytes_in, bytes_out, event_count, chunk_count, content_hash
		FROM session_recordings
		WHERE session_id = $1
		LIMIT 1
	`
	var m RecordingMetadata
	var proto string
	var ended time.Time
	var durMS int64
	err := s.pool.QueryRow(ctx, q, sessionID).Scan(
		&m.SessionID, &m.AgentID, &m.UserID, &proto,
		&m.TerminalSize.Cols, &m.TerminalSize.Rows,
		&m.StartedAt, &ended, &durMS,
		&m.BytesIn, &m.BytesOut, &m.EventCount, &m.ChunkCount, &m.ContentHash,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRecordingNotFound
		}
		return nil, fmt.Errorf("remote: get metadata: %w", err)
	}
	m.Protocol = Protocol(proto)
	m.EndedAt = ended
	m.Duration = (time.Duration(durMS) * time.Millisecond).String()
	return &m, nil
}

// GetRecordingChunks returns all chunks for a session in index order.
// Compressed bytes are returned in their gzip form; the caller can
// decompress with the provided helper DecodeChunk.
func (s *PGStore) GetRecordingChunks(ctx context.Context, sessionID string) ([]RecordingChunk, error) {
	if s == nil || s.pool == nil {
		return nil, errors.New("remote: nil pg store")
	}
	const q = `
		SELECT session_id, chunk_index, event_count, compressed
		FROM session_recording_chunks
		WHERE session_id = $1
		ORDER BY chunk_index ASC
	`
	rows, err := s.pool.Query(ctx, q, sessionID)
	if err != nil {
		return nil, fmt.Errorf("remote: list chunks: %w", err)
	}
	defer rows.Close()

	out := make([]RecordingChunk, 0, 8)
	for rows.Next() {
		var c RecordingChunk
		if err := rows.Scan(&c.SessionID, &c.ChunkIndex, &c.EventCount, &c.Compressed); err != nil {
			return nil, fmt.Errorf("remote: scan chunk: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("remote: rows err: %w", err)
	}
	return out, nil
}

// ListRecordings returns metadata rows matching the filter, plus the
// total count. Admin callers leave AgentID and UserID empty to see
// everything; non-admins should pre-filter by their own UserID.
func (s *PGStore) ListRecordings(ctx context.Context, f RecordingListFilter) ([]RecordingMetadata, int, error) {
	if s == nil || s.pool == nil {
		return nil, 0, errors.New("remote: nil pg store")
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	args := make([]any, 0, 6)
	conds := make([]string, 0, 6)
	add := func(clause string, val any) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(clause, len(args)))
	}
	if f.AgentID != "" {
		add("agent_id = $%d", f.AgentID)
	}
	if f.UserID != "" {
		add("user_id = $%d", f.UserID)
	}
	if f.SessionID != "" {
		add("session_id ILIKE $%d", "%"+f.SessionID+"%")
	}
	if !f.Since.IsZero() {
		add("started_at >= $%d", f.Since)
	}
	if !f.Until.IsZero() {
		add("started_at <= $%d", f.Until)
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM session_recordings "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("remote: count recordings: %w", err)
	}

	args = append(args, f.Limit, f.Offset)
	q := fmt.Sprintf(`
		SELECT session_id, agent_id, user_id, protocol,
		       terminal_cols, terminal_rows,
		       started_at, COALESCE(ended_at, started_at),
		       duration_ms, bytes_in, bytes_out, event_count, chunk_count, content_hash
		FROM session_recordings
		%s
		ORDER BY started_at DESC
		LIMIT $%d OFFSET $%d
	`, where, len(args)-1, len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("remote: list recordings: %w", err)
	}
	defer rows.Close()

	out := make([]RecordingMetadata, 0, f.Limit)
	for rows.Next() {
		var m RecordingMetadata
		var proto string
		var ended time.Time
		var durMS int64
		if err := rows.Scan(
			&m.SessionID, &m.AgentID, &m.UserID, &proto,
			&m.TerminalSize.Cols, &m.TerminalSize.Rows,
			&m.StartedAt, &ended, &durMS,
			&m.BytesIn, &m.BytesOut, &m.EventCount, &m.ChunkCount, &m.ContentHash,
		); err != nil {
			return nil, 0, fmt.Errorf("remote: scan recording: %w", err)
		}
		m.Protocol = Protocol(proto)
		m.EndedAt = ended
		m.Duration = (time.Duration(durMS) * time.Millisecond).String()
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("remote: rows err: %w", err)
	}
	return out, total, nil
}

// DeleteRecording removes a recording and all its chunks. The
// session_recordings row has an ON DELETE CASCADE on chunks so a
// single DELETE suffices.
func (s *PGStore) DeleteRecording(ctx context.Context, sessionID string) error {
	if s == nil || s.pool == nil {
		return errors.New("remote: nil pg store")
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM session_recordings WHERE session_id = $1`, sessionID)
	if err != nil {
		return fmt.Errorf("remote: delete recording: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRecordingNotFound
	}
	return nil
}

// PurgeOlderThan removes recordings whose started_at is older than
// (now - age). Returns the number of rows deleted. Intended for
// retention sweeps.
func (s *PGStore) PurgeOlderThan(ctx context.Context, age time.Duration) (int64, error) {
	if s == nil || s.pool == nil {
		return 0, errors.New("remote: nil pg store")
	}
	cutoff := time.Now().UTC().Add(-age)
	tag, err := s.pool.Exec(ctx, `DELETE FROM session_recordings WHERE started_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("remote: purge recordings: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ErrRecordingNotFound is returned when a session_id has no recording.
var ErrRecordingNotFound = errors.New("remote: recording not found")

// DecodeChunk decompresses a stored chunk and parses its JSON event
// array. Exported for the playback and export endpoints.
func DecodeChunk(c RecordingChunk) ([]RecordingEvent, error) {
	if len(c.Compressed) == 0 {
		return nil, nil
	}
	gz, err := gzip.NewReader(bytes.NewReader(c.Compressed))
	if err != nil {
		return nil, fmt.Errorf("remote: gzip reader: %w", err)
	}
	defer gz.Close()
	raw, err := readAll(gz)
	if err != nil {
		return nil, fmt.Errorf("remote: gzip read: %w", err)
	}
	var evs []RecordingEvent
	if err := json.Unmarshal(raw, &evs); err != nil {
		return nil, fmt.Errorf("remote: json unmarshal: %w", err)
	}
	return evs, nil
}

// readAll is a tiny inlined io.ReadAll that avoids the extra
// allocation of bytes.Buffer in the common chunk path.
func readAll(r interface {
	Read(p []byte) (int, error)
}) ([]byte, error) {
	var buf bytes.Buffer
	tmp := make([]byte, 32*1024)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return buf.Bytes(), nil
			}
			return buf.Bytes(), err
		}
	}
}
