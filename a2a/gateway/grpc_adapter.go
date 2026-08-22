package gateway

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/openagentplatform/openagentplatform/a2a/manager"
	"github.com/openagentplatform/openagentplatform/a2a/models"
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

// ---- status / conversion helpers -------------------------------------------

func modelStatusToProto(taskID, taskStatus string, updated time.Time) *spec.TaskStatusUpdate {
	return &spec.TaskStatusUpdate{
		TaskId:    taskID,
		Status:    modelStatusToProtoEnum(taskStatus),
		UpdatedAt: updated.Format(time.RFC3339),
	}
}

func modelStatusToProtoEnum(status string) spec.TaskStatus {
	switch status {
	case models.TaskStatusPending:
		return spec.TaskStatus_TASK_STATUS_PENDING
	case models.TaskStatusWorking:
		return spec.TaskStatus_TASK_STATUS_WORKING
	case models.TaskStatusInputRequired:
		return spec.TaskStatus_TASK_STATUS_INPUT_REQUIRED
	case models.TaskStatusOutputAvailable:
		return spec.TaskStatus_TASK_STATUS_OUTPUT_AVAILABLE
	case models.TaskStatusCompleted:
		return spec.TaskStatus_TASK_STATUS_COMPLETED
	case models.TaskStatusFailed:
		return spec.TaskStatus_TASK_STATUS_FAILED
	case models.TaskStatusCancelled:
		return spec.TaskStatus_TASK_STATUS_CANCELLED
	default:
		return spec.TaskStatus_TASK_STATUS_UNSPECIFIED
	}
}

func protoStatusToModel(status spec.TaskStatus) string {
	switch status {
	case spec.TaskStatus_TASK_STATUS_PENDING:
		return models.TaskStatusPending
	case spec.TaskStatus_TASK_STATUS_WORKING:
		return models.TaskStatusWorking
	case spec.TaskStatus_TASK_STATUS_INPUT_REQUIRED:
		return models.TaskStatusInputRequired
	case spec.TaskStatus_TASK_STATUS_OUTPUT_AVAILABLE:
		return models.TaskStatusOutputAvailable
	case spec.TaskStatus_TASK_STATUS_COMPLETED:
		return models.TaskStatusCompleted
	case spec.TaskStatus_TASK_STATUS_FAILED:
		return models.TaskStatusFailed
	case spec.TaskStatus_TASK_STATUS_CANCELLED:
		return models.TaskStatusCancelled
	default:
		return models.TaskStatusPending
	}
}

func modelTaskToProto(t *models.Task) *spec.Task {
	var msg *spec.Message
	if t.Message.ID != "" || t.Message.Role != "" || len(t.Message.Parts) > 0 {
		msg = modelMessageToProto(t.Message)
	}
	arts := make([]*spec.Artifact, 0, len(t.Artifacts))
	for i := range t.Artifacts {
		pa := modelArtifactToProto(&t.Artifacts[i])
		arts = append(arts, &pa)
	}
	return &spec.Task{
		Id:        t.ID,
		ContextId: t.ContextID,
		AgentId:   t.AgentID,
		Status:    modelStatusToProtoEnum(t.Status),
		Message:   msg,
		Artifacts: arts,
		Metadata:  t.Metadata,
		Version:   t.Version,
		CreatedAt: t.CreatedAt.Format(time.RFC3339),
		UpdatedAt: t.UpdatedAt.Format(time.RFC3339),
	}
}

func protoTaskToModel(t *spec.Task) *models.Task {
	if t == nil {
		return nil
	}
	out := &models.Task{
		ID:        t.GetId(),
		ContextID: t.GetContextId(),
		AgentID:   t.GetAgentId(),
		Status:    protoStatusToModel(t.GetStatus()),
		Metadata:  t.GetMetadata(),
		Version:   t.GetVersion(),
	}
	if m := t.GetMessage(); m != nil {
		out.Message = protoMessageToModel(m)
	}
	for _, a := range t.GetArtifacts() {
		out.Artifacts = append(out.Artifacts, protoArtifactToModel(a))
	}
	if ts := t.GetCreatedAt(); ts != "" {
		if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			out.CreatedAt = parsed
		}
	}
	if ts := t.GetUpdatedAt(); ts != "" {
		if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			out.UpdatedAt = parsed
		}
	}
	return out
}

func modelMessageToProto(m models.Message) *spec.Message {
	parts := make([]*spec.Part, 0, len(m.Parts))
	for i := range m.Parts {
		parts = append(parts, modelPartToProto(&m.Parts[i]))
	}
	return &spec.Message{Id: m.ID, Role: m.Role, Parts: parts}
}

func protoMessageToModel(m *spec.Message) models.Message {
	if m == nil {
		return models.Message{}
	}
	out := models.Message{ID: m.GetId(), Role: m.GetRole()}
	for _, p := range m.GetParts() {
		out.Parts = append(out.Parts, protoPartToModel(p))
	}
	return out
}

func modelPartToProto(p *models.Part) *spec.Part {
	if p.Text != "" {
		return &spec.Part{Content: &spec.Part_Text{Text: p.Text}}
	}
	if p.File != nil {
		return &spec.Part{Content: &spec.Part_File{File: &spec.FileRef{
			Name: p.File.Name, MimeType: p.File.MimeType, Uri: p.File.URI,
		}}}
	}
	return &spec.Part{}
}

func protoPartToModel(p *spec.Part) models.Part {
	if p == nil {
		return models.Part{}
	}
	switch c := p.Content.(type) {
	case *spec.Part_Text:
		return models.Part{Text: c.Text}
	case *spec.Part_File:
		if c.File != nil {
			return models.Part{File: &models.FileRef{
				Name: c.File.GetName(), MimeType: c.File.GetMimeType(), URI: c.File.GetUri(),
			}}
		}
	}
	return models.Part{}
}

func modelArtifactToProto(a *models.Artifact) spec.Artifact {
	parts := make([]*spec.Part, 0, len(a.Parts))
	for i := range a.Parts {
		parts = append(parts, modelPartToProto(&a.Parts[i]))
	}
	return spec.Artifact{
		Id: a.ID, TaskId: a.TaskID, Name: a.Name, Description: a.Description,
		Parts: parts, MimeType: a.MimeType,
	}
}

func protoArtifactToModel(a *spec.Artifact) models.Artifact {
	out := models.Artifact{
		ID: a.GetId(), TaskID: a.GetTaskId(), Name: a.GetName(),
		Description: a.GetDescription(), MimeType: a.GetMimeType(),
	}
	for _, p := range a.GetParts() {
		out.Parts = append(out.Parts, protoPartToModel(p))
	}
	return out
}

func modelAgentCardToProto(c *models.AgentCard) spec.AgentCard {
	skills := make([]*spec.Skill, 0, len(c.Skills))
	for i := range c.Skills {
		skills = append(skills, &spec.Skill{
			Id: c.Skills[i].ID, Name: c.Skills[i].Name,
			Description: c.Skills[i].Description, Tags: c.Skills[i].Tags,
		})
	}
	var auth *spec.AuthScheme
	if len(c.AuthSchemes) > 0 {
		auth = &spec.AuthScheme{Type: c.AuthSchemes[0].Type, Config: anyMapToStringMap(c.AuthSchemes[0].Config)}
	}
	return spec.AgentCard{
		Id: c.ID, Name: c.Name, Description: c.Description, Version: c.Version,
		Endpoint: c.URL, ProviderName: c.ProviderName, ProviderUrl: c.ProviderURL,
		Streaming: c.Streaming, PushNotifications: c.PushNotifications,
		DefaultInputModes: c.DefaultInputModes, DefaultOutputModes: c.DefaultOutputModes,
		Tags: c.Tags, Skills: skills, Authentication: auth,
	}
}

func anyMapToStringMap(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = fmt.Sprintf("%v", v)
	}
	return out
}

func protoAgentCardToModel(c *spec.AgentCard) models.AgentCard {
	out := models.AgentCard{
		ID: c.GetId(), Name: c.GetName(), Description: c.GetDescription(),
		Version: c.GetVersion(), URL: c.GetEndpoint(),
		ProviderName: c.GetProviderName(), ProviderURL: c.GetProviderUrl(),
		Streaming: c.GetStreaming(), PushNotifications: c.GetPushNotifications(),
		DefaultInputModes: c.GetDefaultInputModes(), DefaultOutputModes: c.GetDefaultOutputModes(),
		Tags: c.GetTags(),
	}
	for _, s := range c.GetSkills() {
		out.Skills = append(out.Skills, models.Skill{
			ID: s.GetId(), Name: s.GetName(), Description: s.GetDescription(), Tags: s.GetTags(),
		})
	}
	if a := c.GetAuthentication(); a != nil {
		out.AuthSchemes = append(out.AuthSchemes, models.AuthScheme{Type: a.GetType(), Config: stringMapToAnyMap(a.GetConfig())})
	}
	return out
}

func stringMapToAnyMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func toGRPCError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrUnauthenticated) {
		return status.Error(codes.Unauthenticated, err.Error())
	}
	if errors.Is(err, ErrPermissionDenied) {
		return status.Error(codes.PermissionDenied, err.Error())
	}
	if s, ok := status.FromError(err); ok {
		return s.Err()
	}
	return status.Error(codes.Internal, err.Error())
}
