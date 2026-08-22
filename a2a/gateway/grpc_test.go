package gateway

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/openagentplatform/openagentplatform/a2a/manager"
	"github.com/openagentplatform/openagentplatform/a2a/models"
	"github.com/openagentplatform/openagentplatform/a2a/registry"
	"github.com/openagentplatform/openagentplatform/a2a/router"
	"github.com/openagentplatform/openagentplatform/a2a/spec"
)

// --- in-memory task Store ----------------------------------------------------

type memStore struct {
	mu       sync.Mutex
	tasks    map[string]*models.Task
	messages map[string][]models.Message
}

func newMemStore() *memStore { return &memStore{tasks: map[string]*models.Task{}, messages: map[string][]models.Message{}} }

func (s *memStore) InsertTask(_ context.Context, t *models.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[t.ID] = t
	return nil
}
func (s *memStore) GetTask(_ context.Context, id string) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, errorsNew("not found")
	}
	return t, nil
}
func (s *memStore) ListTasks(_ context.Context, f manager.TaskFilter) ([]models.Task, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]models.Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		if f.Status != "" && t.Status != f.Status {
			continue
		}
		out = append(out, *t)
	}
	return out, len(out), nil
}
func (s *memStore) UpdateTaskStatus(_ context.Context, id, status string, _ int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tasks[id]; ok {
		t.Status = status
	}
	return nil
}
func (s *memStore) UpdateTask(_ context.Context, t *models.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[t.ID] = t
	return nil
}
func (s *memStore) DeleteTask(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, id)
	return nil
}
func (s *memStore) AddMessage(_ context.Context, taskID string, msg models.Message, _ int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[taskID] = append(s.messages[taskID], msg)
	return nil
}
func (s *memStore) GetMessages(_ context.Context, taskID string) ([]models.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.messages[taskID], nil
}
func (s *memStore) InsertArtifact(_ context.Context, _ *models.Artifact) error { return nil }
func (s *memStore) GetArtifact(_ context.Context, _, _ string) (*models.Artifact, error) {
	return nil, errorsNew("not found")
}
func (s *memStore) ListArtifacts(_ context.Context, _ string) ([]models.Artifact, error) {
	return nil, nil
}
func (s *memStore) DeleteArtifact(_ context.Context, _, _ string) error { return nil }

// --- in-memory CardStore -----------------------------------------------------

type memCardStore struct {
	mu    sync.Mutex
	cards map[string]*registry.AgentCardRow
}

func newMemCardStore() *memCardStore { return &memCardStore{cards: map[string]*registry.AgentCardRow{}} }
func (s *memCardStore) UpsertCard(_ context.Context, c *registry.AgentCardRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cards[c.URL] = c
	return nil
}
func (s *memCardStore) DeleteCard(_ context.Context, url string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cards, url)
	return nil
}
func (s *memCardStore) GetCard(_ context.Context, url string) (*registry.AgentCardRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cards[url]
	if !ok {
		return nil, errorsNew("not found")
	}
	return c, nil
}
func (s *memCardStore) ListCards(_ context.Context) ([]registry.AgentCardRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]registry.AgentCardRow, 0, len(s.cards))
	for _, c := range s.cards {
		out = append(out, *c)
	}
	return out, nil
}

// --- helpers -----------------------------------------------------------------

func errorsNew(msg string) error { return errors.New(msg) }

type testIdentityStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *testIdentityStream) Context() context.Context { return s.ctx }

func newTestGRPCGateway(t *testing.T) *Gateway {
	t.Helper()
	gw, err := NewGateway(
		manager.NewTaskManagerWithStore(newMemStore()),
		newTestRegistry(t),
		newTestRouter(t),
		Config{RequireAuth: false},
	)
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	return gw
}

func newTestRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	r, err := registry.NewRegistry(context.Background(), newMemCardStore(), registry.Config{})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return r
}

func newTestRouter(t *testing.T) *router.Router {
	t.Helper()
	rt, err := router.NewRouter(newTestRegistry(t))
	if err != nil {
		t.Fatalf("router: %v", err)
	}
	return rt
}

func startTestGRPCClient(t *testing.T, gw *Gateway) spec.A2AServiceClient {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	svc := NewGRPCService(gw)
	gs := grpc.NewServer(
		grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			id := &Identity{Subject: "test", Scopes: []string{PermRead, PermSend, PermAdmin}}
			return handler(context.WithValue(ctx, CtxKeyIdentity, id), req)
		}),
		grpc.StreamInterceptor(func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			id := &Identity{Subject: "test", Scopes: []string{PermRead, PermSend, PermAdmin}}
			return handler(srv, &testIdentityStream{ServerStream: ss, ctx: context.WithValue(ss.Context(), CtxKeyIdentity, id)})
		}),
	)
	spec.RegisterA2AServiceServer(gs, svc)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return spec.NewA2AServiceClient(conn)
}

// --- tests -------------------------------------------------------------------

func TestGRPCSendTask(t *testing.T) {
	client := startTestGRPCClient(t, newTestGRPCGateway(t))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := client.SendTask(ctx, &spec.SendTaskRequest{Task: &spec.Task{
		ContextId: "ctx-1",
		AgentId:   "agent-1",
		Message:   &spec.Message{Role: "user", Parts: []*spec.Part{{Content: &spec.Part_Text{Text: "hi"}}}},
	}})
	if err != nil {
		t.Fatalf("SendTask: %v", err)
	}
	if resp.GetTask() == nil || resp.GetTask().GetId() == "" {
		t.Fatalf("expected created task with id, got %+v", resp.GetTask())
	}
}

func TestGRPCGetTask(t *testing.T) {
	client := startTestGRPCClient(t, newTestGRPCGateway(t))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	created, err := client.SendTask(ctx, &spec.SendTaskRequest{Task: &spec.Task{ContextId: "c", AgentId: "a"}})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	got, err := client.GetTask(ctx, &spec.GetTaskRequest{TaskId: created.GetTask().GetId()})
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.GetTask().GetId() != created.GetTask().GetId() {
		t.Fatalf("expected id %q, got %q", created.GetTask().GetId(), got.GetTask().GetId())
	}
}

func TestGRPCListTasks(t *testing.T) {
	client := startTestGRPCClient(t, newTestGRPCGateway(t))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < 3; i++ {
		if _, err := client.SendTask(ctx, &spec.SendTaskRequest{Task: &spec.Task{ContextId: "c", AgentId: "a"}}); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	resp, err := client.ListTasks(ctx, &spec.ListTasksRequest{})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(resp.GetTasks()) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(resp.GetTasks()))
	}
}

func TestGRPCCancelTask(t *testing.T) {
	client := startTestGRPCClient(t, newTestGRPCGateway(t))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	created, err := client.SendTask(ctx, &spec.SendTaskRequest{Task: &spec.Task{ContextId: "c", AgentId: "a"}})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	resp, err := client.CancelTask(ctx, &spec.CancelTaskRequest{TaskId: created.GetTask().GetId()})
	if err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	if !resp.GetSuccess() {
		t.Fatalf("expected success=true")
	}
}

func TestGRPCListAgents(t *testing.T) {
	client := startTestGRPCClient(t, newTestGRPCGateway(t))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Register at least one agent so the response is non-empty.
	_, err := client.RegisterAgent(ctx, &spec.RegisterAgentRequest{Card: &spec.AgentCard{
		Id: "agent-1", Name: "One", Endpoint: "http://localhost:1",
	}})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	resp, err := client.ListAgents(ctx, &spec.ListAgentsRequest{})
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(resp.GetAgents()) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(resp.GetAgents()))
	}
}

func TestGRPCSubscribeTask(t *testing.T) {
	client := startTestGRPCClient(t, newTestGRPCGateway(t))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	created, err := client.SendTask(ctx, &spec.SendTaskRequest{Task: &spec.Task{ContextId: "c", AgentId: "a"}})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	stream, err := client.SubscribeTask(ctx, &spec.SubscribeTaskRequest{TaskId: created.GetTask().GetId()})
	if err != nil {
		t.Fatalf("SubscribeTask: %v", err)
	}
	update, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv: %v", err)
	}
	if update.GetTaskId() != created.GetTask().GetId() {
		t.Fatalf("expected task id %q, got %q", created.GetTask().GetId(), update.GetTaskId())
	}
}
