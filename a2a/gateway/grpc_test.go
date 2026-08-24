package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/spec"
)

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
