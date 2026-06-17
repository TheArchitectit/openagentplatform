// Package gateway - grpc.go implements the gRPC service stub for the
// A2A gateway. It defines the A2AServiceServer interface, server-streaming
// RPCs for task subscriptions, and the interceptor chain (auth, logging,
// recovery, timeout).
//
// This file defines the Go interface contracts and interceptor logic
// without importing the google.golang.org/grpc package, keeping the
// gateway buildable regardless of whether gRPC code generation has been
// run. To activate the gRPC transport, generate protobuf bindings and
// implement the A2AServiceServer interface methods by delegating to the
// Gateway core.
package gateway

import (
	"context"
	"log/slog"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/manager"
	"github.com/openagentplatform/openagentplatform/a2a/models"
)

// ============================================================
// gRPC Service interface
// ============================================================

// A2AServiceServer is the gRPC service definition for the A2A gateway.
// Implementations must be safe for concurrent use.
//
// The service exposes task lifecycle operations and server-streaming
// subscriptions for real-time status updates. When protobuf code is
// generated, this interface will be satisfied by the generated server
// stub; methods receive and return protobuf message types. The method
// signatures here use plain Go types for the contract definition.
type A2AServiceServer interface {
	// SendTask creates and dispatches a new task.
	SendTask(ctx context.Context, req *SendTaskRequest) (*SendTaskResponse, error)

	// GetTask retrieves a task by ID.
	GetTask(ctx context.Context, req *GetTaskRequest) (*GetTaskResponse, error)

	// ListTasks returns filtered tasks.
	ListTasks(ctx context.Context, req *ListTasksRequest) (*ListTasksResponse, error)

	// CancelTask cancels a running task.
	CancelTask(ctx context.Context, req *CancelTaskRequest) (*CancelTaskResponse, error)

	// SendSubscribeTask creates a task and streams status updates.
	// The server sends multiple StatusUpdate messages and closes the
	// stream when the task reaches a terminal state.
	SendSubscribeTask(req *SendTaskRequest, stream A2A_SendSubscribeTaskServer) error

	// SubscribeTask streams status updates for an existing task.
	SubscribeTask(req *SubscribeTaskRequest, stream A2A_SubscribeTaskServer) error

	// RegisterAgent adds an agent card to the registry.
	RegisterAgent(ctx context.Context, req *RegisterAgentRequest) (*RegisterAgentResponse, error)

	// ListAgents returns all registered agents.
	ListAgents(ctx context.Context, req *ListAgentsRequest) (*ListAgentsResponse, error)
}

// ============================================================
// gRPC request/response types
// ============================================================

// SendTaskRequest is the request for SendTask and SendSubscribeTask.
type SendTaskRequest struct {
	Task *models.Task
}

// SendTaskResponse is the response for SendTask.
type SendTaskResponse struct {
	Task *models.Task
}

// GetTaskRequest is the request for GetTask.
type GetTaskRequest struct {
	TaskID string
}

// GetTaskResponse is the response for GetTask.
type GetTaskResponse struct {
	Task *models.Task
}

// ListTasksRequest is the request for ListTasks.
type ListTasksRequest struct {
	SessionID string
	Status    string
	AgentID   string
	Limit     int
	Offset    int
}

// ListTasksResponse is the response for ListTasks.
type ListTasksResponse struct {
	Tasks []models.Task
	Total int
}

// CancelTaskRequest is the request for CancelTask.
type CancelTaskRequest struct {
	TaskID string
}

// CancelTaskResponse is the response for CancelTask.
type CancelTaskResponse struct {
	Success bool
}

// SubscribeTaskRequest is the request for SubscribeTask.
type SubscribeTaskRequest struct {
	TaskID string
}

// RegisterAgentRequest is the request for RegisterAgent.
type RegisterAgentRequest struct {
	Card *models.AgentCard
}

// RegisterAgentResponse is the response for RegisterAgent.
type RegisterAgentResponse struct {
	Card *models.AgentCard
}

// ListAgentsRequest is the request for ListAgents.
type ListAgentsRequest struct{}

// ListAgentsResponse is the response for ListAgents.
type ListAgentsResponse struct {
	Agents []models.AgentCard
}

// ============================================================
// Server stream interfaces
// ============================================================

// A2A_SendSubscribeTaskServer is the server-side stream for SendSubscribeTask.
// The handler sends StatusUpdate messages and closes the stream when done.
type A2A_SendSubscribeTaskServer interface {
	// Send sends a status update to the client.
	Send(update *StatusUpdate) error
	// Context returns the stream's context.
	Context() context.Context
}

// A2A_SubscribeTaskServer is the server-side stream for SubscribeTask.
type A2A_SubscribeTaskServer interface {
	Send(update *StatusUpdate) error
	Context() context.Context
}

// StatusUpdate represents a single task status update in a gRPC stream.
type StatusUpdate struct {
	TaskID    string
	Status    string
	Message   string
	UpdatedAt time.Time
}

// ============================================================
// gRPC Server implementation
// ============================================================

// GRPCServer implements A2AServiceServer by delegating to the Gateway.
type GRPCServer struct {
	UnimplementedA2AServiceServer
	gw *Gateway
}

// UnimplementedA2AServiceServer provides default implementations that
// return Unimplemented. Embed it in your concrete gRPC server to
// ensure forward compatibility.
type UnimplementedA2AServiceServer struct{}

// NewGRPCServer creates a new gRPC server backed by the given gateway.
func NewGRPCServer(gw *Gateway) *GRPCServer {
	return &GRPCServer{gw: gw}
}

// SendTask creates and dispatches a task.
func (s *GRPCServer) SendTask(ctx context.Context, req *SendTaskRequest) (*SendTaskResponse, error) {
	id := IdentityFromGRPCContext(ctx)
	task, err := s.gw.SendTask(ctx, id, req.Task)
	if err != nil {
		return nil, err
	}
	return &SendTaskResponse{Task: task}, nil
}

// GetTask retrieves a task by ID.
func (s *GRPCServer) GetTask(ctx context.Context, req *GetTaskRequest) (*GetTaskResponse, error) {
	id := IdentityFromGRPCContext(ctx)
	task, err := s.gw.GetTask(ctx, id, req.TaskID)
	if err != nil {
		return nil, err
	}
	return &GetTaskResponse{Task: task}, nil
}

// ListTasks returns filtered tasks.
func (s *GRPCServer) ListTasks(ctx context.Context, req *ListTasksRequest) (*ListTasksResponse, error) {
	id := IdentityFromGRPCContext(ctx)
	filter := taskFilterFromGRPCRequest(req)
	tasks, total, err := s.gw.ListTasks(ctx, id, filter)
	if err != nil {
		return nil, err
	}
	return &ListTasksResponse{Tasks: tasks, Total: total}, nil
}

// CancelTask cancels a task.
func (s *GRPCServer) CancelTask(ctx context.Context, req *CancelTaskRequest) (*CancelTaskResponse, error) {
	id := IdentityFromGRPCContext(ctx)
	if err := s.gw.CancelTask(ctx, id, req.TaskID); err != nil {
		return nil, err
	}
	return &CancelTaskResponse{Success: true}, nil
}

// SendSubscribeTask creates a task and streams updates.
func (s *GRPCServer) SendSubscribeTask(req *SendTaskRequest, stream A2A_SendSubscribeTaskServer) error {
	ctx := stream.Context()
	id := IdentityFromGRPCContext(ctx)

	task, err := s.gw.SendTask(ctx, id, req.Task)
	if err != nil {
		return err
	}

	// Send initial update
	if err := stream.Send(&StatusUpdate{
		TaskID:    task.ID,
		Status:    task.Status,
		UpdatedAt: task.UpdatedAt,
	}); err != nil {
		return err
	}

	// Subscribe to subsequent updates
	sub, accepted := s.gw.Hub().Subscribe(task.ID)
	if !accepted {
		return nil
	}
	defer s.gw.Hub().Unsubscribe(sub)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-sub.Events:
			if !ok {
				return nil
			}
			if err := stream.Send(&StatusUpdate{
				TaskID:    event.TaskID,
				Status:    event.Status,
				UpdatedAt: event.UpdatedAt,
			}); err != nil {
				return err
			}
			// Stop streaming on terminal status
			if isTerminalStatus(event.Status) {
				return nil
			}
		}
	}
}

// SubscribeTask streams updates for an existing task.
func (s *GRPCServer) SubscribeTask(req *SubscribeTaskRequest, stream A2A_SubscribeTaskServer) error {
	ctx := stream.Context()
	id := IdentityFromGRPCContext(ctx)
	if err := s.gw.authorize(id, PermRead); err != nil {
		return err
	}

	sub, accepted := s.gw.Hub().Subscribe(req.TaskID)
	if !accepted {
		return nil
	}
	defer s.gw.Hub().Unsubscribe(sub)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-sub.Events:
			if !ok {
				return nil
			}
			if err := stream.Send(&StatusUpdate{
				TaskID:    event.TaskID,
				Status:    event.Status,
				UpdatedAt: event.UpdatedAt,
			}); err != nil {
				return err
			}
			if isTerminalStatus(event.Status) {
				return nil
			}
		}
	}
}

// RegisterAgent adds an agent card.
func (s *GRPCServer) RegisterAgent(ctx context.Context, req *RegisterAgentRequest) (*RegisterAgentResponse, error) {
	id := IdentityFromGRPCContext(ctx)
	if err := s.gw.RegisterAgent(ctx, id, req.Card); err != nil {
		return nil, err
	}
	return &RegisterAgentResponse{Card: req.Card}, nil
}

// ListAgents returns all agents.
func (s *GRPCServer) ListAgents(ctx context.Context, _ *ListAgentsRequest) (*ListAgentsResponse, error) {
	id := IdentityFromGRPCContext(ctx)
	agents, err := s.gw.ListAgents(ctx, id)
	if err != nil {
		return nil, err
	}
	return &ListAgentsResponse{Agents: agents}, nil
}

// ============================================================
// Interceptor definitions
// ============================================================

// GRPCInterceptor is a server-side interceptor function.
// It wraps the handler execution with cross-cutting concerns.
type GRPCInterceptor func(ctx context.Context, req any, info *GRPCMethodInfo, handler GRPCHandler) (resp any, err error)

// GRPCHandler is the actual method handler invoked by an interceptor chain.
type GRPCHandler func(ctx context.Context, req any) (any, error)

// GRPCMethodInfo describes the method being called.
type GRPCMethodInfo struct {
	Service string
	Method  string
}

// AuthInterceptor returns a gRPC interceptor that extracts identity
// from metadata and attaches it to the context.
func AuthInterceptor(auth *Authenticator) GRPCInterceptor {
	return func(ctx context.Context, req any, _ *GRPCMethodInfo, handler GRPCHandler) (any, error) {
		// In a real gRPC setup, extract auth from metadata.MD.
		// This stub attaches nil identity; the gRPC server implementation
		// is responsible for extracting from peer info and headers.
		_ = auth
		return handler(ctx, req)
	}
}

// LoggingInterceptor returns a gRPC interceptor that logs method calls.
func LoggingInterceptor(logger *slog.Logger) GRPCInterceptor {
	return func(ctx context.Context, req any, info *GRPCMethodInfo, handler GRPCHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		logger.LogAttrs(ctx, slog.LevelInfo, "grpc call",
			slog.String("service", info.Service),
			slog.String("method", info.Method),
			slog.Duration("duration", time.Since(start)),
			slog.Bool("error", err != nil),
		)
		return resp, err
	}
}

// RecoveryInterceptor returns a gRPC interceptor that recovers from
// panics and returns an internal error.
func RecoveryInterceptor(logger *slog.Logger) GRPCInterceptor {
	return func(ctx context.Context, req any, info *GRPCMethodInfo, handler GRPCHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("grpc panic recovered",
					slog.Any("panic", r),
					slog.String("method", info.Service+"/"+info.Method),
				)
				err = errInternal("internal error")
				resp = nil
			}
		}()
		return handler(ctx, req)
	}
}

// TimeoutInterceptor returns a gRPC interceptor that applies a timeout
// to the handler context.
func TimeoutInterceptor(timeout time.Duration) GRPCInterceptor {
	return func(ctx context.Context, req any, _ *GRPCMethodInfo, handler GRPCHandler) (any, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return handler(ctx, req)
	}
}

// ChainInterceptors composes multiple interceptors into a single chain.
// The first interceptor is the outermost wrapper.
func ChainInterceptors(interceptors ...GRPCInterceptor) GRPCInterceptor {
	return func(ctx context.Context, req any, info *GRPCMethodInfo, handler GRPCHandler) (any, error) {
		chain := handler
		for i := len(interceptors) - 1; i >= 0; i-- {
			ic := interceptors[i]
			next := chain
			chain = func(ctx context.Context, req any) (any, error) {
				return ic(ctx, req, info, next)
			}
		}
		return chain(ctx, req)
	}
}

// ============================================================
// Helpers
// ============================================================

// IdentityFromGRPCContext extracts the identity from a gRPC context.
// In a real gRPC implementation, this reads from context values set
// by the auth interceptor.
func IdentityFromGRPCContext(ctx context.Context) *Identity {
	if v := ctx.Value(CtxKeyIdentity); v != nil {
		if id, ok := v.(*Identity); ok {
			return id
		}
	}
	return nil
}

// taskFilterFromGRPCRequest converts a gRPC ListTasks request to a manager.TaskFilter.
func taskFilterFromGRPCRequest(req *ListTasksRequest) manager.TaskFilter {
	return manager.TaskFilter{
		SessionID:    req.SessionID,
		Status:       req.Status,
		AgentCardURL: req.AgentID,
		Limit:        req.Limit,
		Offset:       req.Offset,
	}
}

// isTerminalStatus returns true if the status is a terminal task state.
func isTerminalStatus(status string) bool {
	switch status {
	case models.TaskStatusCompleted, models.TaskStatusFailed, models.TaskStatusCancelled:
		return true
	}
	return false
}

// errInternal is a simple error constructor to avoid importing status codes.
type grpcError struct{ msg string }

func (e *grpcError) Error() string { return e.msg }
func errInternal(msg string) error  { return &grpcError{msg: msg} }
