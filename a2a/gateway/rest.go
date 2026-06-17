// Package gateway - rest.go implements the REST+JSON transport for
// the A2A gateway. It exposes task and agent CRUD operations over
// standard HTTP routes, with Server-Sent Events (SSE) for streaming
// task status updates.
package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/manager"
	"github.com/openagentplatform/openagentplatform/a2a/models"
)

// ============================================================
// REST response helpers
// ============================================================

// writeRESTJSON writes a JSON response with the given status code.
func writeRESTJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// ============================================================
// Task endpoints
// ============================================================

// RESTTasksHandler routes /a2a/v1/tasks requests.
//   POST   /a2a/v1/tasks          - create and send a task
//   GET    /a2a/v1/tasks          - list tasks
func RESTTasksHandler(g *Gateway) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := IdentityFromContext(r.Context())
		switch r.Method {
		case http.MethodPost:
			var task models.Task
			if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid request body", RequestIDFromContext(r.Context()))
				return
			}
			created, err := g.SendTask(r.Context(), id, &task)
			if err != nil {
				writeJSONError(w, gatewayErrorStatus(err), err.Error(), RequestIDFromContext(r.Context()))
				return
			}
			writeRESTJSON(w, http.StatusCreated, created)

		case http.MethodGet:
			filter := parseTaskFilter(r)
			tasks, total, err := g.ListTasks(r.Context(), id, filter)
			if err != nil {
				writeJSONError(w, gatewayErrorStatus(err), err.Error(), RequestIDFromContext(r.Context()))
				return
			}
			writeRESTJSON(w, http.StatusOK, ListResult{Tasks: tasks, Total: total})

		default:
			w.Header().Set("Allow", "GET, POST")
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", RequestIDFromContext(r.Context()))
		}
	})
}

// RESTTaskHandler routes /a2a/v1/tasks/{taskId} requests.
//   GET    /a2a/v1/tasks/{taskId}    - get a task
//   DELETE /a2a/v1/tasks/{taskId}    - cancel a task
func RESTTaskHandler(g *Gateway) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		taskID := extractTaskID(r.URL.Path)
		if taskID == "" {
			writeJSONError(w, http.StatusBadRequest, "missing taskId", RequestIDFromContext(r.Context()))
			return
		}

		id := IdentityFromContext(r.Context())
		switch r.Method {
		case http.MethodGet:
			task, err := g.GetTask(r.Context(), id, taskID)
			if err != nil {
				writeJSONError(w, gatewayErrorStatus(err), err.Error(), RequestIDFromContext(r.Context()))
				return
			}
			writeRESTJSON(w, http.StatusOK, task)

		case http.MethodDelete:
			if err := g.CancelTask(r.Context(), id, taskID); err != nil {
				writeJSONError(w, gatewayErrorStatus(err), err.Error(), RequestIDFromContext(r.Context()))
				return
			}
			writeRESTJSON(w, http.StatusOK, map[string]string{"status": "cancelled", "taskId": taskID})

		default:
			w.Header().Set("Allow", "GET, DELETE")
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", RequestIDFromContext(r.Context()))
		}
	})
}

// RESTTaskSubscribeHandler routes /a2a/v1/tasks/{taskId}/subscribe.
//   GET /a2a/v1/tasks/{taskId}/subscribe  - SSE stream of task status updates
func RESTTaskSubscribeHandler(g *Gateway) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		taskID := extractTaskID(r.URL.Path)
		if taskID == "" {
			writeJSONError(w, http.StatusBadRequest, "missing taskId", RequestIDFromContext(r.Context()))
			return
		}

		id := IdentityFromContext(r.Context())
		if err := g.authorize(id, PermRead); err != nil {
			writeJSONError(w, http.StatusForbidden, err.Error(), RequestIDFromContext(r.Context()))
			return
		}

		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "streaming unsupported", RequestIDFromContext(r.Context()))
			return
		}

		sub, accepted := g.Hub().Subscribe(taskID)
		if !accepted {
			writeJSONError(w, http.StatusServiceUnavailable, "max connections reached", RequestIDFromContext(r.Context()))
			return
		}
		defer g.Hub().Unsubscribe(sub)

		// Send initial connection event
		fmt.Fprintf(w, "event: connected\ndata: {\"taskId\":\"%s\"}\n\n", taskID)
		flusher.Flush()

		// Stream events until client disconnects
		for {
			select {
			case <-r.Context().Done():
				return
			case event, ok := <-sub.Events:
				if !ok {
					return
				}
				data, _ := json.Marshal(event)
				fmt.Fprintf(w, "event: status\ndata: %s\n\n", data)
				flusher.Flush()
			}
		}
	})
}

// ============================================================
// Agent endpoints
// ============================================================

// RESTAgentsHandler routes /a2a/v1/agents requests.
//   POST /a2a/v1/agents          - register an agent
//   GET  /a2a/v1/agents          - list agents
func RESTAgentsHandler(g *Gateway) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := IdentityFromContext(r.Context())
		switch r.Method {
		case http.MethodPost:
			var card models.AgentCard
			if err := json.NewDecoder(r.Body).Decode(&card); err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid request body", RequestIDFromContext(r.Context()))
				return
			}
			if err := g.RegisterAgent(r.Context(), id, &card); err != nil {
				writeJSONError(w, gatewayErrorStatus(err), err.Error(), RequestIDFromContext(r.Context()))
				return
			}
			writeRESTJSON(w, http.StatusCreated, card)

		case http.MethodGet:
			agents, err := g.ListAgents(r.Context(), id)
			if err != nil {
				writeJSONError(w, gatewayErrorStatus(err), err.Error(), RequestIDFromContext(r.Context()))
				return
			}
			writeRESTJSON(w, http.StatusOK, ListAgentsResult{Agents: agents})

		default:
			w.Header().Set("Allow", "GET, POST")
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", RequestIDFromContext(r.Context()))
		}
	})
}

// RESTAgentHandler routes /a2a/v1/agents/{url} requests.
//   GET    /a2a/v1/agents/{url}    - get an agent
//   DELETE /a2a/v1/agents/{url}    - deregister an agent
//   PUT    /a2a/v1/agents/{url}    - update an agent
func RESTAgentHandler(g *Gateway) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agentURL := extractAgentURL(r.URL.Path)
		if agentURL == "" {
			writeJSONError(w, http.StatusBadRequest, "missing agent url", RequestIDFromContext(r.Context()))
			return
		}

		id := IdentityFromContext(r.Context())
		switch r.Method {
		case http.MethodGet:
			agents, err := g.ListAgents(r.Context(), id)
			if err != nil {
				writeJSONError(w, gatewayErrorStatus(err), err.Error(), RequestIDFromContext(r.Context()))
				return
			}
			for _, a := range agents {
				if a.Endpoint == agentURL {
					writeRESTJSON(w, http.StatusOK, a)
					return
				}
			}
			writeJSONError(w, http.StatusNotFound, "agent not found", RequestIDFromContext(r.Context()))

		case http.MethodDelete:
			if err := g.authorize(id, PermAdmin); err != nil {
				writeJSONError(w, http.StatusForbidden, err.Error(), RequestIDFromContext(r.Context()))
				return
			}
			// Deregistration is not currently supported by the registry.
			// Return 501 to indicate the operation is known but not implemented.
			writeJSONError(w, http.StatusNotImplemented, "agent deregistration not supported", RequestIDFromContext(r.Context()))

		case http.MethodPut:
			if err := g.authorize(id, PermAdmin); err != nil {
				writeJSONError(w, http.StatusForbidden, err.Error(), RequestIDFromContext(r.Context()))
				return
			}
			var card models.AgentCard
			if err := json.NewDecoder(r.Body).Decode(&card); err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid request body", RequestIDFromContext(r.Context()))
				return
			}
			card.Endpoint = agentURL
			if err := g.RegisterAgent(r.Context(), id, &card); err != nil {
				writeJSONError(w, gatewayErrorStatus(err), err.Error(), RequestIDFromContext(r.Context()))
				return
			}
			writeRESTJSON(w, http.StatusOK, card)

		default:
			w.Header().Set("Allow", "GET, PUT, DELETE")
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", RequestIDFromContext(r.Context()))
		}
	})
}

// ============================================================
// Health endpoint
// ============================================================

// RESTHealthHandler handles GET /a2a/v1/health.
func RESTHealthHandler(w http.ResponseWriter, r *http.Request) {
	writeRESTJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// ============================================================
// Path parsing helpers
// ============================================================

// extractTaskID extracts the task ID from a URL path like
// /a2a/v1/tasks/{taskId} or /a2a/v1/tasks/{taskId}/subscribe.
func extractTaskID(path string) string {
	// Remove trailing /subscribe if present
	path = strings.TrimSuffix(path, "/subscribe")
	// Path: /a2a/v1/tasks/{taskId}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 && parts[len(parts)-2] == "tasks" {
		return parts[len(parts)-1]
	}
	// Path: /tasks/{taskId}
	if len(parts) >= 2 && parts[len(parts)-2] == "tasks" {
		return parts[len(parts)-1]
	}
	// Fallback: last segment
	if len(parts) > 0 {
		last := parts[len(parts)-1]
		if last != "tasks" && last != "agents" {
			return last
		}
	}
	return ""
}

// extractAgentURL extracts the agent URL from a path like
// /a2a/v1/agents/{url}.
func extractAgentURL(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 2 {
		last := parts[len(parts)-1]
		if last != "agents" {
			return last
		}
	}
	return ""
}

// parseTaskFilter parses query parameters into a TaskFilter.
func parseTaskFilter(r *http.Request) manager.TaskFilter {
	q := r.URL.Query()
	f := manager.TaskFilter{
		SessionID:    q.Get("sessionId"),
		Status:       q.Get("status"),
		AgentCardURL: q.Get("agentUrl"),
	}
	return f
}

// gatewayErrorStatus maps gateway errors to HTTP status codes.
func gatewayErrorStatus(err error) int {
	switch err.Error() {
	case ErrPermissionDenied.Error():
		return http.StatusForbidden
	case ErrRateLimited.Error():
		return http.StatusTooManyRequests
	case ErrUnauthenticated.Error():
		return http.StatusUnauthorized
	}
	// Check sentinel errors
	if err.Error() == models.ErrTaskNotFound.Error() {
		return http.StatusNotFound
	}
	if err.Error() == models.ErrInvalidTransition.Error() {
		return http.StatusConflict
	}
	if err.Error() == models.ErrNoMatchingAgent.Error() {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}
