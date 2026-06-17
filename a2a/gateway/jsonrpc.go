// Package gateway - jsonrpc.go implements the JSON-RPC 2.0 transport
// for the A2A gateway. It exposes 7 methods over a single POST endpoint
// using the standard JSON-RPC 2.0 envelope.
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/openagentplatform/openagentplatform/a2a/manager"
	"github.com/openagentplatform/openagentplatform/a2a/models"
)

// ============================================================
// JSON-RPC 2.0 error codes
// ============================================================

const (
	// JSON-RPC standard error codes
	ErrCodeParse          = -32700 // Invalid JSON
	ErrCodeInvalidRequest = -32600 // Invalid JSON-RPC envelope
	ErrCodeMethodNotFound = -32601 // Method does not exist
	ErrCodeInvalidParams  = -32602 // Invalid method parameters
	ErrCodeInternal       = -32603 // Internal error

	// A2A-specific error codes (application-defined range)
	ErrCodePermission     = -32001 // Permission denied
	ErrCodeRateLimited    = -32002 // Rate limit exceeded
	ErrCodeUnauthenticated = -32003 // Not authenticated
	ErrCodeTaskNotFound   = -32004 // Task not found
	ErrCodeAgentNotFound  = -32005 // No matching agent
	ErrCodeInvalidState   = -32006 // Invalid task state transition
)

// ============================================================
// JSON-RPC 2.0 envelope
// ============================================================

// JSONRPCRequest is the standard JSON-RPC 2.0 request envelope.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
}

// JSONRPCResponse is the standard JSON-RPC 2.0 response envelope.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  any             `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
	ID      json.RawMessage `json:"id"`
}

// JSONRPCError represents a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// ============================================================
// Method names
// ============================================================

const (
	MethodTasksSend         = "tasks/send"
	MethodTasksSendSubscribe = "tasks/sendSubscribe"
	MethodTasksGet          = "tasks/get"
	MethodTasksList         = "tasks/list"
	MethodTasksCancel       = "tasks/cancel"
	MethodAgentRegister     = "agent/register"
	MethodAgentList         = "agent/list"
)

// ============================================================
// Parameters / Results
// ============================================================

// SendParams are the parameters for tasks/send.
type SendParams struct {
	Task *models.Task `json:"task"`
}

// GetParams are the parameters for tasks/get.
type GetParams struct {
	TaskID string `json:"taskId"`
}

// ListParams are the parameters for tasks/list.
type ListParams struct {
	Filter manager.TaskFilter `json:"filter,omitempty"`
}

// ListResult is the return value for tasks/list.
type ListResult struct {
	Tasks []models.Task `json:"tasks"`
	Total int           `json:"total"`
}

// CancelParams are the parameters for tasks/cancel.
type CancelParams struct {
	TaskID string `json:"taskId"`
}

// RegisterAgentParams are the parameters for agent/register.
type RegisterAgentParams struct {
	Card *models.AgentCard `json:"card"`
}

// ListAgentsResult is the return value for agent/list.
type ListAgentsResult struct {
	Agents []models.AgentCard `json:"agents"`
}

// ============================================================
// JSON-RPC handler
// ============================================================

// JSONRPCHandler returns an http.Handler that serves JSON-RPC 2.0
// requests against the given gateway.
func JSONRPCHandler(g *Gateway) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONRPCError(w, nil, ErrCodeInvalidRequest, "POST required", nil)
			return
		}

		var req JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONRPCError(w, nil, ErrCodeParse, "parse error", nil)
			return
		}

		if req.JSONRPC != "2.0" {
			writeJSONRPCError(w, req.ID, ErrCodeInvalidRequest, "jsonrpc must be \"2.0\"", nil)
			return
		}

		id := IdentityFromContext(r.Context())
		ctx := r.Context()

		result, errCode, errMsg, errData := g.dispatchJSONRPC(ctx, id, req)
		if errCode != 0 {
			writeJSONRPCError(w, req.ID, errCode, errMsg, errData)
			return
		}

		writeJSONRPCResult(w, req.ID, result)
	})
}

// dispatchJSONRPC routes a JSON-RPC request to the appropriate handler.
func (g *Gateway) dispatchJSONRPC(ctx context.Context, id *Identity, req JSONRPCRequest) (result any, errCode int, errMsg string, errData any) {
	switch req.Method {
	case MethodTasksSend:
		return g.jsonrpcSend(ctx, id, req.Params)
	case MethodTasksSendSubscribe:
		return g.jsonrpcSendSubscribe(ctx, id, req.Params)
	case MethodTasksGet:
		return g.jsonrpcGet(ctx, id, req.Params)
	case MethodTasksList:
		return g.jsonrpcList(ctx, id, req.Params)
	case MethodTasksCancel:
		return g.jsonrpcCancel(ctx, id, req.Params)
	case MethodAgentRegister:
		return g.jsonrpcAgentRegister(ctx, id, req.Params)
	case MethodAgentList:
		return g.jsonrpcAgentList(ctx, id, req.Params)
	default:
		return nil, ErrCodeMethodNotFound, fmt.Sprintf("method not found: %s", req.Method), nil
	}
}

// ============================================================
// Method implementations
// ============================================================

func (g *Gateway) jsonrpcSend(ctx context.Context, id *Identity, raw json.RawMessage) (any, int, string, any) {
	var p SendParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, ErrCodeInvalidParams, "invalid params", nil
	}
	task, err := g.SendTask(ctx, id, p.Task)
	if err != nil {
		return nil, mapGatewayError(err), err.Error(), nil
	}
	return task, 0, "", nil
}

func (g *Gateway) jsonrpcSendSubscribe(ctx context.Context, id *Identity, raw json.RawMessage) (any, int, string, any) {
	var p SendParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, ErrCodeInvalidParams, "invalid params", nil
	}
	task, err := g.SendTask(ctx, id, p.Task)
	if err != nil {
		return nil, mapGatewayError(err), err.Error(), nil
	}
	// Note: actual SSE streaming is handled by the REST layer (subscribe endpoint).
	// JSON-RPC returns the initial task; clients use the SSE endpoint for updates.
	return task, 0, "", nil
}

func (g *Gateway) jsonrpcGet(ctx context.Context, id *Identity, raw json.RawMessage) (any, int, string, any) {
	var p GetParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, ErrCodeInvalidParams, "invalid params", nil
	}
	task, err := g.GetTask(ctx, id, p.TaskID)
	if err != nil {
		return nil, mapGatewayError(err), err.Error(), nil
	}
	return task, 0, "", nil
}

func (g *Gateway) jsonrpcList(ctx context.Context, id *Identity, raw json.RawMessage) (any, int, string, any) {
	var p ListParams
	if raw != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, ErrCodeInvalidParams, "invalid params", nil
		}
	}
	tasks, total, err := g.ListTasks(ctx, id, p.Filter)
	if err != nil {
		return nil, mapGatewayError(err), err.Error(), nil
	}
	return ListResult{Tasks: tasks, Total: total}, 0, "", nil
}

func (g *Gateway) jsonrpcCancel(ctx context.Context, id *Identity, raw json.RawMessage) (any, int, string, any) {
	var p CancelParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, ErrCodeInvalidParams, "invalid params", nil
	}
	if err := g.CancelTask(ctx, id, p.TaskID); err != nil {
		return nil, mapGatewayError(err), err.Error(), nil
	}
	return map[string]string{"status": "cancelled", "taskId": p.TaskID}, 0, "", nil
}

func (g *Gateway) jsonrpcAgentRegister(ctx context.Context, id *Identity, raw json.RawMessage) (any, int, string, any) {
	var p RegisterAgentParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, ErrCodeInvalidParams, "invalid params", nil
	}
	if err := g.RegisterAgent(ctx, id, p.Card); err != nil {
		return nil, mapGatewayError(err), err.Error(), nil
	}
	return map[string]string{"status": "registered", "endpoint": p.Card.URL}, 0, "", nil
}

func (g *Gateway) jsonrpcAgentList(ctx context.Context, id *Identity, _ json.RawMessage) (any, int, string, any) {
	agents, err := g.ListAgents(ctx, id)
	if err != nil {
		return nil, mapGatewayError(err), err.Error(), nil
	}
	return ListAgentsResult{Agents: agents}, 0, "", nil
}

// ============================================================
// Helpers
// ============================================================

// mapGatewayError maps gateway errors to JSON-RPC error codes.
func mapGatewayError(err error) int {
	switch {
	case errors.Is(err, ErrPermissionDenied):
		return ErrCodePermission
	case errors.Is(err, ErrRateLimited):
		return ErrCodeRateLimited
	case errors.Is(err, ErrUnauthenticated):
		return ErrCodeUnauthenticated
	case errors.Is(err, models.ErrTaskNotFound):
		return ErrCodeTaskNotFound
	case errors.Is(err, models.ErrNoMatchingAgent):
		return ErrCodeAgentNotFound
	case errors.Is(err, models.ErrInvalidTransition):
		return ErrCodeInvalidState
	default:
		return ErrCodeInternal
	}
}

// writeJSONRPCResult writes a successful JSON-RPC response.
func writeJSONRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		Result:  result,
		ID:      id,
	}
	json.NewEncoder(w).Encode(resp)
}

// writeJSONRPCError writes a JSON-RPC error response.
func writeJSONRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string, data any) {
	w.Header().Set("Content-Type", "application/json")
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
		ID: id,
	}
	json.NewEncoder(w).Encode(resp)
}
