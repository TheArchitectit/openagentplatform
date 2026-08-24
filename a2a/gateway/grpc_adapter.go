package gateway

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/openagentplatform/openagentplatform/a2a/manager"
	"github.com/openagentplatform/openagentplatform/a2a/spec"
)

// grpcService implements the generated A2AServiceServer by delegating to the
// Gateway core. Identity is expected to already be on the context (set by the
// gRPC auth interceptor in buildGRPCServer); it is read with IdentityFromContext.
type grpcService struct {
	spec.UnimplementedA2AServiceServer
	gw     *Gateway
	logger Logger
}

// Logger is the minimal logging surface the adapter needs.
type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// NewGRPCService creates a gRPC service backed by the given gateway.
func NewGRPCService(gw *Gateway) *grpcService {
	return &grpcService{gw: gw}
}

// SetLogger attaches a logger to the service.
func (s *grpcService) SetLogger(l Logger) { s.logger = l }

func (s *grpcService) identity(ctx context.Context) (*Identity, error) {
	if id := IdentityFromContext(ctx); id != nil {
		return id, nil
	}
	return nil, status.Error(codes.Unauthenticated, "unauthenticated")
}

func (s *grpcService) SendTask(ctx context.Context, req *spec.SendTaskRequest) (*spec.SendTaskResponse, error) {
	id, err := s.identity(ctx)
	if err != nil {
		return nil, err
	}
	if req.GetTask() == nil {
		return nil, status.Error(codes.InvalidArgument, "task required")
	}
	task, err := s.gw.SendTask(ctx, id, protoTaskToModel(req.GetTask()))
	if err != nil {
		return nil, toGRPCError(err)
	}
	return &spec.SendTaskResponse{Task: modelTaskToProto(task)}, nil
}

func (s *grpcService) GetTask(ctx context.Context, req *spec.GetTaskRequest) (*spec.GetTaskResponse, error) {
	id, err := s.identity(ctx)
	if err != nil {
		return nil, err
	}
	task, err := s.gw.GetTask(ctx, id, req.GetTaskId())
	if err != nil {
		return nil, toGRPCError(err)
	}
	return &spec.GetTaskResponse{Task: modelTaskToProto(task)}, nil
}

func (s *grpcService) ListTasks(ctx context.Context, req *spec.ListTasksRequest) (*spec.ListTasksResponse, error) {
	id, err := s.identity(ctx)
	if err != nil {
		return nil, err
	}
	filter := manager.TaskFilter{
		SessionID:    req.GetSessionId(),
		Status:       req.GetStatus(),
		AgentCardURL: req.GetAgentId(),
		Limit:        int(req.GetLimit()),
		Offset:       int(req.GetOffset()),
	}
	tasks, total, err := s.gw.ListTasks(ctx, id, filter)
	if err != nil {
		return nil, toGRPCError(err)
	}
	out := make([]*spec.Task, 0, len(tasks))
	for i := range tasks {
		out = append(out, modelTaskToProto(&tasks[i]))
	}
	return &spec.ListTasksResponse{Tasks: out, Total: int32(total)}, nil
}

func (s *grpcService) CancelTask(ctx context.Context, req *spec.CancelTaskRequest) (*spec.CancelTaskResponse, error) {
	id, err := s.identity(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.gw.CancelTask(ctx, id, req.GetTaskId()); err != nil {
		return nil, toGRPCError(err)
	}
	return &spec.CancelTaskResponse{Success: true}, nil
}

func (s *grpcService) SendSubscribeTask(req *spec.SendTaskRequest, stream spec.A2AService_SendSubscribeTaskServer) error {
	ctx := stream.Context()
	id, err := s.identity(ctx)
	if err != nil {
		return err
	}
	if req.GetTask() == nil {
		return status.Error(codes.InvalidArgument, "task required")
	}
	task, err := s.gw.SendTask(ctx, id, protoTaskToModel(req.GetTask()))
	if err != nil {
		return toGRPCError(err)
	}
	if err := stream.Send(modelStatusToProto(task.ID, task.Status, task.UpdatedAt)); err != nil {
		return err
	}
	return s.streamTask(ctx, task.ID, stream.Send)
}

func (s *grpcService) SubscribeTask(req *spec.SubscribeTaskRequest, stream spec.A2AService_SubscribeTaskServer) error {
	ctx := stream.Context()
	id, err := s.identity(ctx)
	if err != nil {
		return err
	}
	if err := s.gw.authorize(id, PermRead); err != nil {
		return toGRPCError(err)
	}
	return s.streamTask(ctx, req.GetTaskId(), stream.Send)
}

func (s *grpcService) streamTask(ctx context.Context, taskID string, send func(*spec.TaskStatusUpdate) error) error {
	sub, accepted := s.gw.Hub().Subscribe(taskID)
	if !accepted {
		return status.Error(codes.ResourceExhausted, "max connections reached")
	}
	defer s.gw.Hub().Unsubscribe(sub)
	// Send an initial status update so the client can distinguish
	// "connected, no events yet" from a hard failure.
	if t, err := s.gw.GetTaskInternal(ctx, taskID); err == nil && t != nil {
		if err := send(modelStatusToProto(taskID, t.Status, t.UpdatedAt)); err != nil {
			return err
		}
		if isTerminalStatus(t.Status) {
			return nil
		}
	}
	for {
		select {
		case <-sub.Done:
			return nil
		case event, ok := <-sub.Events:
			if !ok {
				return nil
			}
			if err := send(modelStatusToProto(event.TaskID, event.Status, event.UpdatedAt)); err != nil {
				return err
			}
			if isTerminalStatus(event.Status) {
				return nil
			}
		}
	}
}

func (s *grpcService) RegisterAgent(ctx context.Context, req *spec.RegisterAgentRequest) (*spec.RegisterAgentResponse, error) {
	id, err := s.identity(ctx)
	if err != nil {
		return nil, err
	}
	if req.GetCard() == nil {
		return nil, status.Error(codes.InvalidArgument, "card required")
	}
	card := protoAgentCardToModel(req.GetCard())
	if err := s.gw.RegisterAgent(ctx, id, &card); err != nil {
		return nil, toGRPCError(err)
	}
	return &spec.RegisterAgentResponse{Card: req.GetCard()}, nil
}

func (s *grpcService) ListAgents(ctx context.Context, _ *spec.ListAgentsRequest) (*spec.ListAgentsResponse, error) {
	id, err := s.identity(ctx)
	if err != nil {
		return nil, err
	}
	agents, err := s.gw.ListAgents(ctx, id)
	if err != nil {
		return nil, toGRPCError(err)
	}
	out := make([]*spec.AgentCard, 0, len(agents))
	for i := range agents {
		pc := modelAgentCardToProto(&agents[i])
		out = append(out, &pc)
	}
	return &spec.ListAgentsResponse{Agents: out}, nil
}
