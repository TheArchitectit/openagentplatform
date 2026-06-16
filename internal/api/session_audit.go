// HTTP API for shell session recordings.
//
// All routes live under /api/v1/shell/recordings. The recorder itself
// is wired into the shell bridge (see internal/api/remote.go); this
// file is read-only + delete for the stored recordings and exposes
// a Server-Sent Events stream for playback.

package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/remote"
)

// SetRecordingStore wires the recording persistence interface into
// the server. Called from main; may be nil — endpoints then return 503.
func (s *Server) SetRecordingStore(store remote.SessionRecordingStore) {
	s.recordingStore = store
}

// SetSessionRecorderFactory wires a factory that produces recorders
// for a given session id. The bridge calls this when it needs to
// attach a recorder to a live session.
func (s *Server) SetSessionRecorderFactory(f func(sessionID string) (*remote.SessionRecorder, bool)) {
	s.recorderFactory = f
}

// handleListRecordings handles GET /api/v1/shell/recordings.
//
// Query parameters:
//
//	agent_id, user_id, session_id (substring), since, until
//	limit, offset
func (s *Server) handleListRecordings(w http.ResponseWriter, r *http.Request) {
	if s.recordingStore == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "recording_store_unavailable")
		return
	}
	claims, _ := auth.UserFromContext(r.Context())
	q := r.URL.Query()
	f := remote.RecordingListFilter{
		AgentID:   q.Get("agent_id"),
		UserID:    q.Get("user_id"),
		SessionID: q.Get("session_id"),
	}
	if claims != nil && !isAdminRole(claims.Role) {
		// Non-admins are scoped to their own recordings; the caller
		// can also narrow further with the other params.
		f.UserID = claims.Subject
	}
	if v := q.Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Since = t
		}
	}
	if v := q.Get("until"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Until = t
		}
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.Limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.Offset = n
		}
	}

	items, total, err := s.recordingStore.ListRecordings(r.Context(), f)
	if err != nil {
		s.log.Error("list recordings failed", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "list_recordings_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"recordings": items,
		"total":      total,
		"limit":      f.Limit,
		"offset":     f.Offset,
	})
}

// handleGetRecording handles GET /api/v1/shell/recordings/{session_id}.
// Returns metadata plus chunk summary info so the UI can decide
// whether to stream playback or download the export.
func (s *Server) handleGetRecording(w http.ResponseWriter, r *http.Request) {
	if s.recordingStore == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "recording_store_unavailable")
		return
	}
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "session_id_required")
		return
	}
	meta, err := s.recordingStore.GetRecordingMetadata(r.Context(), sessionID)
	if err != nil {
		if errors.Is(err, remote.ErrRecordingNotFound) {
			writeJSONError(w, http.StatusNotFound, "recording_not_found")
			return
		}
		s.log.Error("get recording failed", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "get_recording_failed")
		return
	}
	if !s.canAccessRecording(r, meta) {
		writeJSONError(w, http.StatusForbidden, "recording_forbidden")
		return
	}
	chunks, err := s.recordingStore.GetRecordingChunks(r.Context(), sessionID)
	if err != nil {
		s.log.Error("get recording chunks failed", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "get_chunks_failed")
		return
	}
	totalEvents := 0
	for _, c := range chunks {
		totalEvents += c.EventCount
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"metadata":    meta,
		"chunk_count": len(chunks),
		"event_count": totalEvents,
		"has_play":    len(chunks) > 0,
	})
}

// handleDeleteRecording handles DELETE /api/v1/shell/recordings/{session_id}.
// Hard delete. Admin only.
func (s *Server) handleDeleteRecording(w http.ResponseWriter, r *http.Request) {
	if s.recordingStore == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "recording_store_unavailable")
		return
	}
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || !isAdminRole(claims.Role) {
		writeJSONError(w, http.StatusForbidden, "admin_required")
		return
	}
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "session_id_required")
		return
	}
	if err := s.recordingStore.DeleteRecording(r.Context(), sessionID); err != nil {
		if errors.Is(err, remote.ErrRecordingNotFound) {
			writeJSONError(w, http.StatusNotFound, "recording_not_found")
			return
		}
		s.log.Error("delete recording failed", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "delete_recording_failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePlayRecording handles GET /api/v1/shell/recordings/{session_id}/play.
//
// Streams the recording as Server-Sent Events. The browser EventSource
// client receives one SSE "event: data" message per recording event,
// with a JSON payload of {t, dir, data} where t is the event's
// monotonic offset in milliseconds from session start, dir is "in" or
// "out", and data is base64-encoded raw bytes.
//
// Query parameters:
//
//	speed   playback speed multiplier; default 1.0
//	from    starting offset in ms (for resume); default 0
//	format  "sse" (default) or "json-array" (single JSON object with
//	        all events; useful for "save and load")
func (s *Server) handlePlayRecording(w http.ResponseWriter, r *http.Request) {
	if s.recordingStore == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "recording_store_unavailable")
		return
	}
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "session_id_required")
		return
	}
	meta, err := s.recordingStore.GetRecordingMetadata(r.Context(), sessionID)
	if err != nil {
		if errors.Is(err, remote.ErrRecordingNotFound) {
			writeJSONError(w, http.StatusNotFound, "recording_not_found")
			return
		}
		s.log.Error("get recording failed", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "get_recording_failed")
		return
	}
	if !s.canAccessRecording(r, meta) {
		writeJSONError(w, http.StatusForbidden, "recording_forbidden")
		return
	}

	speed := 1.0
	if v := r.URL.Query().Get("speed"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f <= 64 {
			speed = f
		}
	}
	fromMS := int64(0)
	if v := r.URL.Query().Get("from"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			fromMS = n
		}
	}

	// If the caller wants the whole recording as a single JSON array,
	// collect and return it. This is used by the frontend to
	// "render" the whole session into xterm.js without streaming.
	if r.URL.Query().Get("format") == "json-array" {
		s.streamRecordingAsJSON(w, r, sessionID)
		return
	}

	chunks, err := s.recordingStore.GetRecordingChunks(r.Context(), sessionID)
	if err != nil {
		s.log.Error("list chunks failed", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "list_chunks_failed")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming_unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	startWall := time.Now()
	base := meta.StartedAt
	for _, c := range chunks {
		events, err := remote.DecodeChunk(c)
		if err != nil {
			s.log.Warn("decode chunk failed", "session_id", sessionID, "idx", c.ChunkIndex, "err", err)
			continue
		}
		for _, ev := range events {
			offsetMS := ev.Timestamp.Sub(base).Milliseconds()
			if offsetMS < fromMS {
				continue
			}
			// Wait in real time scaled by 1/speed.
			// We compute the absolute target time and sleep until then.
			// Events with identical timestamps are flushed together.
			elapsed := time.Since(startWall)
			target := time.Duration(float64(offsetMS-fromMS)*1000/speed) * time.Microsecond
			if target > elapsed {
				select {
				case <-time.After(target - elapsed):
				case <-r.Context().Done():
					return
				}
			}
			payload, _ := json.Marshal(map[string]any{
				"t":    offsetMS,
				"dir":  string(ev.Direction),
				"data": ev.Data,
				"size": ev.Size,
			})
			if _, err := fmt.Fprintf(w, "event: data\ndata: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
	// End-of-stream marker.
	_, _ = fmt.Fprintf(w, "event: end\ndata: {}\n\n")
	flusher.Flush()
}

// streamRecordingAsJSON returns all events in a single JSON envelope.
// Each event is base64-encoded in `data_b64` (the raw playback data)
// plus offset_ms for the timeline. Used by the playback UI when
// streaming is undesirable (e.g. small recordings, or the client
// wants a single fetch and then renders locally with timer events).
func (s *Server) streamRecordingAsJSON(w http.ResponseWriter, r *http.Request, sessionID string) {
	chunks, err := s.recordingStore.GetRecordingChunks(r.Context(), sessionID)
	if err != nil {
		s.log.Error("list chunks failed", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "list_chunks_failed")
		return
	}
	meta, err := s.recordingStore.GetRecordingMetadata(r.Context(), sessionID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "get_metadata_failed")
		return
	}
	type playEvent struct {
		OffsetMS int64    `json:"offset_ms"`
		Dir      string   `json:"dir"`
		DataB64  string   `json:"data_b64"`
		DataHex  string   `json:"data_hex"`
		Size     int      `json:"size"`
		WallTS   string   `json:"wall_ts"`
	}
	events := make([]playEvent, 0, 256)
	for _, c := range chunks {
		decoded, err := remote.DecodeChunk(c)
		if err != nil {
			s.log.Warn("decode chunk failed", "session_id", sessionID, "idx", c.ChunkIndex, "err", err)
			continue
		}
		for _, ev := range decoded {
			raw, _ := remote.DecodeForJSON(ev.Data)
			events = append(events, playEvent{
				OffsetMS: ev.Timestamp.Sub(meta.StartedAt).Milliseconds(),
				Dir:      string(ev.Direction),
				DataB64:  base64.StdEncoding.EncodeToString(raw),
				DataHex:  ev.Data,
				Size:     ev.Size,
				WallTS:   ev.Timestamp.Format(time.RFC3339Nano),
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"metadata": meta,
		"events":   events,
	})
}

// handleExportRecording handles GET /api/v1/shell/recordings/{session_id}/export.
//
// Returns the recording as an asciinema .cast v2 file. The asciinema
// format is a JSON-line stream: the first line is the header
// object, subsequent lines are timestamped events of the form
// [elapsed, "o", data].
//
// https://github.com/asciinema/asciinema/blob/develop/doc/asciicast-v2.md
func (s *Server) handleExportRecording(w http.ResponseWriter, r *http.Request) {
	if s.recordingStore == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "recording_store_unavailable")
		return
	}
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "session_id_required")
		return
	}
	meta, err := s.recordingStore.GetRecordingMetadata(r.Context(), sessionID)
	if err != nil {
		if errors.Is(err, remote.ErrRecordingNotFound) {
			writeJSONError(w, http.StatusNotFound, "recording_not_found")
			return
		}
		s.log.Error("get recording failed", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "get_recording_failed")
		return
	}
	if !s.canAccessRecording(r, meta) {
		writeJSONError(w, http.StatusForbidden, "recording_forbidden")
		return
	}
	chunks, err := s.recordingStore.GetRecordingChunks(r.Context(), sessionID)
	if err != nil {
		s.log.Error("list chunks failed", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "list_chunks_failed")
		return
	}

	w.Header().Set("Content-Type", "application/x-asciicast")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s.cast"`, sessionID))
	// Header line.
	startedUnix := float64(meta.StartedAt.UnixNano()) / 1e9
	header := map[string]any{
		"version": 2,
		"width":  meta.TerminalSize.Cols,
		"height": meta.TerminalSize.Rows,
		"timestamp": int64(meta.StartedAt.Unix()),
		"env": map[string]string{
			"SHELL": "/bin/sh",
			"TERM":  "xterm-256color",
		},
		"title": fmt.Sprintf("oap shell session %s on %s", sessionID, meta.AgentID),
	}
	if meta.TerminalSize.Cols <= 0 {
		header["width"] = 80
	}
	if meta.TerminalSize.Rows <= 0 {
		header["height"] = 24
	}
	_ = json.NewEncoder(w).Encode(header)
	_ = startedUnix // referenced for future duration calculations
	// Event lines. Only "out" (agent → user) data is exported by
	// default since asciinema records output, not input.
	for _, c := range chunks {
		events, err := remote.DecodeChunk(c)
		if err != nil {
			s.log.Warn("decode chunk failed", "session_id", sessionID, "idx", c.ChunkIndex, "err", err)
			continue
		}
		for _, ev := range events {
			if ev.Direction != remote.DirOut {
				continue
			}
			raw, err := remote.DecodeForJSON(ev.Data)
			if err != nil {
				continue
			}
			elapsed := ev.Timestamp.Sub(meta.StartedAt).Seconds()
			if elapsed < 0 {
				elapsed = 0
			}
			line := []any{elapsed, "o", string(raw)}
			if err := json.NewEncoder(w).Encode(line); err != nil {
				return
			}
		}
	}
}

// canAccessRecording enforces RBAC: admins see everything; non-admins
// only see their own recordings.
func (s *Server) canAccessRecording(r *http.Request, m *remote.RecordingMetadata) bool {
	claims, _ := auth.UserFromContext(r.Context())
	if claims == nil {
		return false
	}
	if isAdminRole(claims.Role) {
		return true
	}
	return claims.Subject == m.UserID
}

// recordRetentionPurge is a small helper used by the optional
// background retenter. We expose it so the operator can call it from
// a cron-style job; it's a thin wrapper around the store's
// PurgeOlderThan.
func (s *Server) recordRetentionPurge(age time.Duration) (int64, error) {
	if s.recordingStore == nil {
		return 0, errors.New("recording_store_unavailable")
	}
	return s.recordingStore.PurgeOlderThan(nil, age)
}

// splitPath is a tiny utility used when we need to inspect the
// session-id path segment directly (kept for future expansion of
// recording routes that need to inspect multiple segments).
func splitPath(p string) []string {
	return strings.Split(strings.Trim(p, "/"), "/")
}
