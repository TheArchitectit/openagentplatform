# A2A Subsystem Implementation Plan

**Domain:** a2a (Agent-to-Agent Protocol Gateway)
**Version:** 1.0.0
**Status:** Implementation-Ready
**Date:** 2026-06-15
**Target Release:** Q3 2026

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Architecture Overview](#2-architecture-overview)
3. [Directory Structure](#3-directory-structure)
4. [Proto Definitions](#4-proto-definitions)
5. [Component Specifications](#5-component-specifications)
   - 5.1 [Task Lifecycle State Machine](#51-task-lifecycle-state-machine)
   - 5.2 [Agent Card Registry](#52-agent-card-registry)
   - 5.3 [Gateway Routing Engine](#53-gateway-routing-engine)
   - 5.4 [Protocol Bindings](#54-protocol-bindings)
   - 5.5 [Task Persistence](#55-task-persistence)
   - 5.6 [Push Notification System](#56-push-notification-system)
   - 5.7 [Authentication & Authorization](#57-authentication--authorization)
   - 5.8 [Event-to-Task Bridge](#58-event-to-task-bridge)
   - 5.9 [Streaming Support](#59-streaming-support)
   - 5.10 [Error Handling](#510-error-handling)
   - 5.11 [Gateway Scaling](#511-gateway-scaling)
   - 5.12 [A2A Test Suite](#512-a2a-test-suite)
6. [Data Models (SQL DDL)](#6-data-models-sql-ddl)
7. [API Schemas (Full JSON)](#7-api-schemas-full-json)
8. [Configuration](#8-configuration)
9. [Dependencies](#9-dependencies)
10. [Implementation Steps](#10-implementation-steps-ordered)
11. [Verification Checklist](#11-verification-checklist)

---

## 1. Executive Summary

The A2A subsystem is the agent-to-agent protocol gateway for OpenAgentPlatform. It provides:

- A **Task lifecycle state machine** managing agent tasks from submission through completion, cancellation, or failure.
- An **Agent Card registry** storing discovery metadata at `/.well-known/agent-card.json`.
- A **routing engine** that dispatches tasks to registered agents based on skill matching and capability constraints.
- **Three protocol bindings**: JSON-RPC 2.0 over HTTP/SSE, gRPC server-streaming, and REST+JSON+SSE.
- **PostgreSQL-backed persistence** with full ACID guarantees and optimistic concurrency control.
- A **push notification system** using webhooks with HMAC-SHA256 signing and exponential-backoff retry.
- **Pluggable authentication** supporting bearer tokens, mutual TLS, and OAuth 2.1.
- An **event-to-task bridge** that converts RMM platform events (alert, ticket, deploy, scan) into A2A Tasks.
- **Horizontal scaling** via stateless gateway instances behind a load balancer with shared PostgreSQL state.
- A **comprehensive test suite** covering unit, integration, contract, and load tests.

The subsystem is implemented in Go 1.23, uses the Echo HTTP framework, pgx for PostgreSQL, and protoc-generated code for gRPC.

---

## 2. Architecture Overview

```
                        ┌─────────────────────────────────────────┐
                        │            A2A Gateway Service           │
                        │                                          │
   ┌──────────┐         │  ┌─────────────┐   ┌─────────────────┐  │
   │ External │────────►│  │  HTTP/JSON  │   │   gRPC Server   │  │
   │  Client  │  JSON-  │  │  RPC Router │   │   (port 8443)   │  │
   │          │  RPC    │  └──────┬──────┘   └────────┬────────┘  │
   │          │────────►│         │                   │            │
   │          │  REST   │  ┌──────▼──────┐   ┌────────▼────────┐  │
   │          │────────►│  │  REST+SSE   │   │  SSE Broadcaster│  │
   └──────────┘         │  │  Router     │   │  (in-memory +   │  │
                        │  └──────┬──────┘   │   Redis pubsub) │  │
                        │         │            └─────────────────┘  │
                        │  ┌──────▼──────────────────────────────┐  │
                        │  │       Task Lifecycle State Machine   │  │
                        │  │  (submitted → working → completed)   │  │
                        │  └──────┬──────────────────────────────┘  │
                        │         │                                  │
                        │  ┌──────▼──────┐   ┌──────────────────┐   │
                        │  │  Agent Card │   │  Routing Engine  │   │
                        │  │  Registry   │◄──┤  (skill match)   │   │
                        │  └─────────────┘   └──────────────────┘   │
                        │                                              │
                        │  ┌─────────────┐  ┌──────────────────────┐  │
                        │  │  Event-to-  │  │  Push Notification   │  │
                        │  │  Task Bridge│  │  Dispatcher          │  │
                        │  └─────────────┘  └──────────────────────┘  │
                        │                                              │
                        │  ┌──────────────────────────────────────┐  │
                        │  │     PostgreSQL (pgx pool)            │  │
                        │  │  tasks | artifacts | agent_cards |   │  │
                        │  │  push_webhooks | events_inbox        │  │
                        │  └──────────────────────────────────────┘  │
                        └─────────────────────────────────────────┘
```

---

## 3. Directory Structure

All new files live under `a2a/` at the repository root, parallel to `mcp-server/`. The Go module path is `github.com/thearchitectit/a2a-gateway`.

```
a2a/
├── go.mod
├── go.sum
├── cmd/
│   └── a2a-gateway/
│       └── main.go                    # Service entrypoint
├── proto/
│   ├── a2a.proto                      # Protocol buffer definitions
│   ├── buf.gen.yaml                   # buf codegen config
│   └── Makefile                       # protoc wrapper
├── internal/
│   ├── config/
│   │   └── config.go                  # Env-based configuration
│   ├── task/
│   │   ├── state_machine.go           # Task state transitions
│   │   ├── state_machine_test.go
│   │   ├── manager.go                 # Task CRUD orchestrator
│   │   ├── manager_test.go
│   │   ├── types.go                   # Task, Artifact, Part types
│   │   └── repository.go              # PostgreSQL persistence
│   ├── agentcard/
│   │   ├── types.go                   # AgentCard, Skill, Capability
│   │   ├── registry.go                # In-memory + DB-backed registry
│   │   ├── registry_test.go
│   │   ├── discover.go                # /.well-known/agent-card.json
│   │   └── repository.go
│   ├── routing/
│   │   ├── engine.go                  # Skill-matching router
│   │   ├── engine_test.go
│   │   └── types.go
│   ├── transport/
│   │   ├── http/
│   │   │   ├── server.go              # Echo HTTP server
│   │   │   ├── jsonrpc.go             # JSON-RPC 2.0 handler
│   │   │   ├── jsonrpc_test.go
│   │   │   ├── rest.go                # REST+JSON endpoints
│   │   │   ├── rest_test.go
│   │   │   ├── sse.go                 # Server-Sent Events
│   │   │   ├── sse_test.go
│   │   │   └── middleware.go          # Auth, rate limit, logging
│   │   └── grpc/
│   │       ├── server.go              # gRPC server
│   │       ├── server_test.go
│   │       └── stream.go              # Server-streaming handlers
│   ├── pb/                            # Generated protobuf code
│   │   ├── a2a.pb.go
│   │   ├── a2a_grpc.pb.go
│   │   └── a2a.pb.gw.go               # gRPC-Gateway (optional)
│   ├── bridge/
│   │   ├── event_bridge.go            # RMM event → A2A Task converter
│   │   ├── event_bridge_test.go
│   │   ├── handlers.go                # Per-event-type handlers
│   │   └── mapping.go                 # Event-type → skill mappings
│   ├── pushnotify/
│   │   ├── dispatcher.go              # Webhook delivery engine
│   │   ├── dispatcher_test.go
│   │   ├── signing.go                 # HMAC-SHA256 signing
│   │   ├── retry.go                   # Exponential backoff
│   │   └── types.go
│   ├── auth/
│   │   ├── bearer.go                  # Bearer token validation
│   │   ├── bearer_test.go
│   │   ├── mtls.go                    # Mutual TLS verification
│   │   ├── mtls_test.go
│   │   ├── oauth.go                   # OAuth 2.1 token introspection
│   │   ├── oauth_test.go
│   │   └── interface.go               # Authenticator interface
│   ├── errors/
│   │   ├── codes.go                   # A2A error code definitions
│   │   ├── errors.go                  # Structured error type
│   │   └── errors_test.go
│   ├── observability/
│   │   ├── metrics.go                 # Prometheus collectors
│   │   ├── tracing.go                 # OpenTelemetry setup
│   │   └── logging.go                 # Structured logger
│   └── scaling/
│       ├── leader.go                  # Leader election (optional)
│       └── shutdown.go                # Graceful shutdown
├── migrations/
│   ├── 001_create_tasks.up.sql
│   ├── 001_create_tasks.down.sql
│   ├── 002_create_artifacts.up.sql
│   ├── 002_create_artifacts.down.sql
│   ├── 003_create_agent_cards.up.sql
│   ├── 003_create_agent_cards.down.sql
│   ├── 004_create_push_webhooks.up.sql
│   ├── 004_create_push_webhooks.down.sql
│   ├── 005_create_events_inbox.up.sql
│   ├── 005_create_events_inbox.down.sql
│   ├── 006_create_indexes.up.sql
│   ├── 006_create_indexes.down.sql
│   └── verify_schema.sql
├── tests/
│   ├── integration/
│   │   ├── task_lifecycle_test.go
│   │   ├── agent_card_test.go
│   │   ├── routing_test.go
│   │   ├── jsonrpc_test.go
│   │   ├── rest_test.go
│   │   ├── sse_test.go
│   │   ├── grpc_test.go
│   │   ├── push_notify_test.go
│   │   ├── event_bridge_test.go
│   │   ├── auth_test.go
│   │   └── error_handling_test.go
│   ├── contract/
│   │   ├── jsonrpc_schema_test.go
│   │   ├── rest_schema_test.go
│   │   └── a2a_proto_compliance_test.go
│   ├── load/
│   │   ├── k6_throughput.js
│   │   ├── k6_streaming.js
│   │   └── vegeta_targets.txt
│   └── helpers/
│       ├── testdb.go
│       ├── testserver.go
│       └── fixtures.go
├── deploy/
│   ├── Dockerfile
│   ├── docker-compose.yml             # gateway + postgres + redis
│   ├── gateway-deployment.yaml        # Kubernetes
│   ├── gateway-service.yaml
│   ├── gateway-ingress.yaml
│   └── hpa.yaml                       # HorizontalPodAutoscaler
├── docs/
│   ├── a2a-overview.md
│   ├── agent-card-spec.md
│   ├── task-lifecycle.md
│   ├── api-reference.md
│   └── deployment.md
└── Makefile                           # Build, test, lint, proto targets
```

---

## 4. Proto Definitions

### File: `a2a/proto/a2a.proto`

```protobuf
syntax = "proto3";

package a2a.v1;

option go_package = "github.com/thearchitectit/a2a-gateway/internal/pb;a2apb";

import "google/protobuf/timestamp.proto";
import "google/protobuf/struct.proto";

// ============================================================================
// Core Types
// ============================================================================

message Part {
  oneof content {
    TextPart  text  = 1;
    FilePart  file  = 2;
    DataPart  data  = 3;
  }
}

message TextPart {
  string text = 1;
  string mime_type = 2;  // default "text/plain"
}

message FilePart {
  string name     = 1;
  string mime_type = 2;
  oneof source {
    bytes inline_bytes = 3;
    string uri         = 4;
  }
}

message DataPart {
  google.protobuf.Struct data = 1;
}

message Artifact {
  string   artifact_id = 1;
  string   name        = 2;
  string   description = 3;
  repeated Part parts   = 4;
  google.protobuf.Timestamp created_at = 5;
  map<string, string> metadata = 6;
}

// ============================================================================
// Task State Machine
// ============================================================================

enum TaskState {
  TASK_STATE_UNSPECIFIED = 0;
  TASK_STATE_SUBMITTED   = 1;
  TASK_STATE_WORKING     = 2;
  TASK_STATE_INPUT_REQUIRED = 3;
  TASK_STATE_COMPLETED   = 4;
  TASK_STATE_FAILED      = 5;
  TASK_STATE_CANCELED    = 6;
}

// ============================================================================
// Task
// ============================================================================

message Task {
  string   id          = 1;
  string   context_id  = 2;
  TaskState state      = 3;
  Message  message     = 4;  // last/initial message
  repeated Artifact artifacts = 5;
  google.protobuf.Timestamp created_at  = 6;
  google.protobuf.Timestamp updated_at  = 7;
  google.protobuf.Timestamp completed_at = 8;
  string   agent_id    = 9;
  string   session_id  = 10;
  map<string, string> metadata = 11;
  int32    version     = 12;  // optimistic concurrency
}

message Message {
  string   message_id  = 1;
  string   role        = 2;  // "user" | "agent"
  repeated Part parts   = 3;
  google.protobuf.Timestamp created_at = 4;
  map<string, string> metadata = 5;
}

// ============================================================================
// Agent Card
// ============================================================================

message AgentCard {
  string   agent_id      = 1;
  string   name          = 2;
  string   description   = 3;
  string   url           = 4;
  string   version       = 5;
  string   provider      = 6;
  string   documentation = 7;
  repeated AgentSkill   skills          = 8;
  repeated AgentCapability capabilities = 9;
  repeated string       default_input_modes  = 10;
  repeated string       default_output_modes = 11;
  google.protobuf.Timestamp created_at   = 12;
  google.protobuf.Timestamp updated_at   = 13;
}

message AgentSkill {
  string   id          = 1;
  string   name        = 2;
  string   description = 3;
  repeated string tags  = 4;
  repeated string examples = 5;
  repeated string input_modes  = 6;
  repeated string output_modes = 7;
}

message AgentCapability {
  string name        = 1;
  string version     = 2;
  bool   streaming   = 3;
  bool   push_notifications = 4;
}

// ============================================================================
// RPC Messages
// ============================================================================

message SendTaskRequest {
  string  agent_id  = 1;
  Message message   = 2;
  string  session_id = 3;
  map<string, string> metadata = 4;
  // If true, server returns a stream of Task updates instead of final Task.
  bool    stream    = 5;
}

message SendTaskResponse {
  Task task = 1;
}

message GetTaskRequest {
  string task_id = 1;
  int32  history_length = 2;  // 0 = no history
}

message GetTaskResponse {
  Task   task   = 1;
  repeated Message history = 2;
}

message CancelTaskRequest {
  string task_id = 1;
}

message CancelTaskResponse {
  Task task = 1;
}

message SubscribeTaskRequest {
  string task_id = 1;
}

message ListTasksRequest {
  string   agent_id  = 1;
  TaskState state    = 2;
  int32    page_size = 3;
  string   page_token = 4;
}

message ListTasksResponse {
  repeated Task tasks = 1;
  string   next_page_token = 2;
}

message RegisterAgentRequest {
  AgentCard card = 1;
}

message RegisterAgentResponse {
  AgentCard card = 1;
}

message DiscoverAgentsRequest {
  string skill_tag = 1;  // optional filter
  string capability = 2; // optional filter
}

message DiscoverAgentsResponse {
  repeated AgentCard cards = 1;
}

// ============================================================================
// Service
// ============================================================================

service A2AService {
  // Unary task submission
  rpc SendTask       (SendTaskRequest)        returns (SendTaskResponse);
  rpc GetTask        (GetTaskRequest)         returns (GetTaskResponse);
  rpc CancelTask     (CancelTaskRequest)      returns (CancelTaskResponse);
  rpc ListTasks      (ListTasksRequest)       returns (ListTasksResponse);

  // Server-streaming subscription
  rpc SubscribeTask  (SubscribeTaskRequest)   returns (stream Task);

  // Agent Card management
  rpc RegisterAgent  (RegisterAgentRequest)   returns (RegisterAgentResponse);
  rpc DiscoverAgents (DiscoverAgentsRequest)  returns (DiscoverAgentsResponse);
}
```

### File: `a2a/proto/buf.gen.yaml`

```yaml
version: v1
plugins:
  - plugin: go
    out: internal/pb
    opt:
      - paths=source_relative
  - plugin: go-grpc
    out: internal/pb
    opt:
      - paths=source_relative
```

### File: `a2a/proto/Makefile`

```makefile
.PHONY: gen clean

gen:
	protoc \
		--proto_path=. \
		--go_out=../internal/pb --go_opt=paths=source_relative \
		--go-grpc_out=../internal/pb --go-grpc_opt=paths=source_relative \
		a2a.proto

clean:
	rm -f ../internal/pb/a2a.pb.go ../internal/pb/a2a_grpc.pb.go
```

**Implementation steps for proto compilation:**

1. Install `protoc` (v25.1+), `protoc-gen-go` (v1.34.2), `protoc-gen-go-grpc` (v1.5.1).
2. Write `a2a/proto/a2a.proto` with content above.
3. Write `a2a/proto/buf.gen.yaml`.
4. Write `a2a/proto/Makefile`.
5. Run `make -C a2a/proto gen` to produce `a2a/internal/pb/a2a.pb.go` and `a2a/internal/pb/a2a_grpc.pb.go`.
6. Verify generated package name is `a2apb`.

---

## 5. Component Specifications

### 5.1 Task Lifecycle State Machine

**Purpose:** Enforce valid state transitions for A2A Tasks, preventing illegal moves (e.g., `completed → working`).

**File: `a2a/internal/task/types.go`**

Complete type definitions:

```go
package task

import (
	"encoding/json"
	"time"
)

type TaskState string

const (
	TaskStateUnspecified     TaskState = "unspecified"
	TaskStateSubmitted        TaskState = "submitted"
	TaskStateWorking          TaskState = "working"
	TaskStateInputRequired    TaskState = "input-required"
	TaskStateCompleted        TaskState = "completed"
	TaskStateFailed           TaskState = "failed"
	TaskStateCanceled         TaskState = "canceled"
)

func (s TaskState) Valid() bool {
	switch s {
	case TaskStateSubmitted, TaskStateWorking, TaskStateInputRequired,
		TaskStateCompleted, TaskStateFailed, TaskStateCanceled:
		return true
	}
	return false
}

func (s TaskState) Terminal() bool {
	return s == TaskStateCompleted || s == TaskStateFailed || s == TaskStateCanceled
}

type Part struct {
	Kind     string          `json:"kind"`     // "text" | "file" | "data"
	Text     string          `json:"text,omitempty"`
	FileName string          `json:"file_name,omitempty"`
	MimeType string          `json:"mime_type,omitempty"`
	URI      string          `json:"uri,omitempty"`
	Bytes    []byte          `json:"bytes,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
}

type Message struct {
	MessageID string    `json:"message_id"`
	Role      string    `json:"role"` // "user" | "agent"
	Parts     []Part    `json:"parts"`
	CreatedAt time.Time `json:"created_at"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type Artifact struct {
	ArtifactID string    `json:"artifact_id"`
	Name       string    `json:"name"`
	Description string   `json:"description"`
	Parts      []Part    `json:"parts"`
	CreatedAt  time.Time `json:"created_at"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type Task struct {
	ID          string     `json:"id"`
	ContextID   string     `json:"context_id"`
	State       TaskState  `json:"state"`
	Message     Message    `json:"message"`
	Artifacts   []Artifact `json:"artifacts"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	AgentID     string     `json:"agent_id"`
	SessionID   string     `json:"session_id"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Version     int32      `json:"version"`
}
```

**File: `a2a/internal/task/state_machine.go`**

State transition table and validator:

```go
package task

import "fmt"

// transitionTable maps (from, to) -> bool.
// Unlisted transitions are invalid.
var transitionTable = map[TaskState]map[TaskState]bool{
	TaskStateSubmitted: {
		TaskStateWorking:       true,
		TaskStateInputRequired: true,
		TaskStateCompleted:     true, // synchronous short-circuit
		TaskStateFailed:        true,
		TaskStateCanceled:      true,
	},
	TaskStateWorking: {
		TaskStateInputRequired: true,
		TaskStateWorking:       true, // re-entrant: agent sends progress update
		TaskStateCompleted:     true,
		TaskStateFailed:        true,
		TaskStateCanceled:      true,
	},
	TaskStateInputRequired: {
		TaskStateWorking:    true,
		TaskStateCompleted:  true,
		TaskStateFailed:     true,
		TaskStateCanceled:   true,
	},
	TaskStateCompleted: {}, // terminal
	TaskStateFailed:    {}, // terminal
	TaskStateCanceled:  {}, // terminal
}

// CanTransition reports whether moving from `from` to `to` is permitted.
func CanTransition(from, to TaskState) bool {
	if !from.Valid() || !to.Valid() {
		return false
	}
	allowed, ok := transitionTable[from]
	if !ok {
		return false
	}
	return allowed[to]
}

// ValidateTransition returns a descriptive error if the transition is illegal.
func ValidateTransition(from, to TaskState) error {
	if !from.Valid() {
		return fmt.Errorf("invalid source state: %q", from)
	}
	if !to.Valid() {
		return fmt.Errorf("invalid target state: %q", to)
	}
	if from.Terminal() {
		return fmt.Errorf("cannot transition from terminal state %q", from)
	}
	if !CanTransition(from, to) {
		return fmt.Errorf("illegal transition: %q -> %q", from, to)
	}
	return nil
}
```

**Service logic:** The `Manager` (see 5.5) calls `ValidateTransition` before any UPDATE. State changes are applied inside a transaction with `SELECT ... FOR UPDATE` to prevent race conditions.

**Error handling:** Illegal transitions return `ErrInvalidStateTransition` (error code `-32002`).

**Tests:** `a2a/internal/task/state_machine_test.go` covers:
- `TestCanTransition_AllValidTransitions` — verify all 14 valid edges.
- `TestCanTransition_InvalidTransitions` — verify terminal states block all outgoing edges.
- `TestCanTransition_ReentrantWorking` — `working → working` is allowed for progress updates.
- `TestValidateTransition_Errors` — returns specific error for each invalid pair.
- `TestTaskStateValid_AllStates` — every defined constant validates.
- `TestTaskStateTerminal_TrueForCompletedFailedCanceled` — terminal property.

---

### 5.2 Agent Card Registry

**Purpose:** Store and discover agent capability metadata. Expose at `/.well-known/agent-card.json`.

**File: `a2a/internal/agentcard/types.go`**

```go
package agentcard

import "time"

type AgentCard struct {
	AgentID            string            `json:"agent_id"`
	Name               string            `json:"name"`
	Description        string            `json:"description"`
	URL                string            `json:"url"`
	Version            string            `json:"version"`
	Provider           string            `json:"provider"`
	Documentation      string            `json:"documentation,omitempty"`
	Skills             []AgentSkill      `json:"skills"`
	Capabilities       []AgentCapability `json:"capabilities"`
	DefaultInputModes  []string          `json:"default_input_modes"`
	DefaultOutputModes []string          `json:"default_output_modes"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
}

type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"input_modes"`
	OutputModes []string `json:"output_modes"`
}

type AgentCapability struct {
	Name             string `json:"name"`
	Version          string `json:"version"`
	Streaming        bool   `json:"streaming"`
	PushNotifications bool  `json:"push_notifications"`
}
```

**File: `a2a/internal/agentcard/registry.go`**

```go
package agentcard

import (
	"context"
	"sync"
	"time"
)

// Registry is an in-memory + DB-backed agent card store.
// Cards are loaded from PostgreSQL on startup and kept in sync.
type Registry struct {
	mu    sync.RWMutex
	cards map[string]*AgentCard // agent_id -> card
	db    Repository
}

func NewRegistry(db Repository) *Registry {
	return &Registry{cards: make(map[string]*AgentCard), db: db}
}

// Register inserts or updates an agent card.
func (r *Registry) Register(ctx context.Context, card *AgentCard) error {
	if card.AgentID == "" {
		return ErrEmptyAgentID
	}
	if len(card.Skills) == 0 {
		return ErrNoSkills
	}
	now := time.Now().UTC()
	if card.CreatedAt.IsZero() {
		card.CreatedAt = now
	}
	card.UpdatedAt = now

	if err := r.db.Upsert(ctx, card); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cards[card.AgentID] = card
	return nil
}

// Get retrieves a card by agent ID.
func (r *Registry) Get(agentID string) (*AgentCard, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.cards[agentID]
	return c, ok
}

// DiscoverBySkill returns cards whose skills contain a matching tag.
func (r *Registry) DiscoverBySkill(tag string) []*AgentCard {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*AgentCard
	for _, c := range r.cards {
		for _, s := range c.Skills {
			for _, t := range s.Tags {
				if t == tag {
					out = append(out, c)
					goto next
				}
			}
		}
	next:
	}
	return out
}

// DiscoverByCapability returns cards that declare the given capability.
func (r *Registry) DiscoverByCapability(name string) []*AgentCard {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*AgentCard
	for _, c := range r.cards {
		for _, cap := range c.Capabilities {
			if cap.Name == name {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

// LoadAll hydrates the in-memory map from the database.
func (r *Registry) LoadAll(ctx context.Context) error {
	cards, err := r.db.List(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range cards {
		r.cards[c.AgentID] = c
	}
	return nil
}

// Count returns the number of registered cards.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.cards)
}
```

**File: `a2a/internal/agentcard/discover.go`**

Serves the well-known endpoint:

```go
package agentcard

import (
	"encoding/json"
	"net/http"
)

// WellKnownHandler returns an http.HandlerFunc that serves
// /.well-known/agent-card.json for a single self-describing agent.
func WellKnownHandler(card *AgentCard) http.HandlerFunc {
	body, _ := json.Marshal(card)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}
}
```

**File: `a2a/internal/agentcard/repository.go`**

PostgreSQL persistence layer (see full SQL in section 6).

```go
package agentcard

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	Upsert(ctx context.Context, c *AgentCard) error
	Get(ctx context.Context, agentID string) (*AgentCard, error)
	List(ctx context.Context) ([]*AgentCard, error)
	Delete(ctx context.Context, agentID string) error
}

type pgRepository struct {
	pool *pgxpool.Pool
}

func NewPGRepository(pool *pgxpool.Pool) Repository {
	return &pgRepository{pool: pool}
}

func (r *pgRepository) Upsert(ctx context.Context, c *AgentCard) error {
	skills, _ := json.Marshal(c.Skills)
	caps, _ := json.Marshal(c.Capabilities)
	inputModes, _ := json.Marshal(c.DefaultInputModes)
	outputModes, _ := json.Marshal(c.DefaultOutputModes)

	const q = `
		INSERT INTO agent_cards (
			agent_id, name, description, url, version, provider, documentation,
			skills, capabilities, default_input_modes, default_output_modes,
			created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (agent_id) DO UPDATE SET
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			url = EXCLUDED.url,
			version = EXCLUDED.version,
			provider = EXCLUDED.provider,
			documentation = EXCLUDED.documentation,
			skills = EXCLUDED.skills,
			capabilities = EXCLUDED.capabilities,
			default_input_modes = EXCLUDED.default_input_modes,
			default_output_modes = EXCLUDED.default_output_modes,
			updated_at = EXCLUDED.updated_at
	`
	_, err := r.pool.Exec(ctx, q,
		c.AgentID, c.Name, c.Description, c.URL, c.Version, c.Provider, c.Documentation,
		skills, caps, inputModes, outputModes,
		c.CreatedAt, c.UpdatedAt,
	)
	return err
}

func (r *pgRepository) Get(ctx context.Context, agentID string) (*AgentCard, error) {
	const q = `
		SELECT agent_id, name, description, url, version, provider, documentation,
		       skills, capabilities, default_input_modes, default_output_modes,
		       created_at, updated_at
		FROM agent_cards WHERE agent_id = $1
	`
	row := r.pool.QueryRow(ctx, q, agentID)
	var c AgentCard
	var skills, caps, in, out []byte
	err := row.Scan(
		&c.AgentID, &c.Name, &c.Description, &c.URL, &c.Version, &c.Provider, &c.Documentation,
		&skills, &caps, &in, &out,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("agent card not found: %w", err)
	}
	json.Unmarshal(skills, &c.Skills)
	json.Unmarshal(caps, &c.Capabilities)
	json.Unmarshal(in, &c.DefaultInputModes)
	json.Unmarshal(out, &c.DefaultOutputModes)
	return &c, nil
}

func (r *pgRepository) List(ctx context.Context) ([]*AgentCard, error) {
	const q = `
		SELECT agent_id, name, description, url, version, provider, documentation,
		       skills, capabilities, default_input_modes, default_output_modes,
		       created_at, updated_at
		FROM agent_cards ORDER BY name
	`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*AgentCard
	for rows.Next() {
		var c AgentCard
		var skills, caps, in, outb []byte
		if err := rows.Scan(
			&c.AgentID, &c.Name, &c.Description, &c.URL, &c.Version, &c.Provider, &c.Documentation,
			&skills, &caps, &in, &outb,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		json.Unmarshal(skills, &c.Skills)
		json.Unmarshal(caps, &c.Capabilities)
		json.Unmarshal(in, &c.DefaultInputModes)
		json.Unmarshal(outb, &c.DefaultOutputModes)
		out = append(out, &c)
	}
	return out, rows.Err()
}

func (r *pgRepository) Delete(ctx context.Context, agentID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM agent_cards WHERE agent_id = $1`, agentID)
	return err
}
```

**Errors:**

```go
package agentcard

import "errors"

var (
	ErrEmptyAgentID = errors.New("agent_id is required")
	ErrNoSkills     = errors.New("agent must declare at least one skill")
	ErrNotFound     = errors.New("agent card not found")
)
```

**Tests:** `a2a/internal/agentcard/registry_test.go`:
- `TestRegister_NewCard` — adds card, verifies in map and DB.
- `TestRegister_UpdateExisting` — upsert overwrites fields, preserves created_at.
- `TestRegister_EmptyAgentID` — returns ErrEmptyAgentID.
- `TestRegister_NoSkills` — returns ErrNoSkills.
- `TestGet_Existing` — returns card.
- `TestGet_Missing` — returns false.
- `TestDiscoverBySkill` — returns matching cards.
- `TestDiscoverByCapability` — returns matching cards.
- `TestLoadAll_FromDB` — hydrates in-memory from DB fixture.
- `TestCount` — returns correct count after multiple registers.

**Configuration:** None (registry operates from DB; no per-instance env vars).

---

### 5.3 Gateway Routing Engine

**Purpose:** Match incoming task requests to the best agent based on skill tags, capabilities, and load.

**File: `a2a/internal/routing/types.go`**

```go
package routing

import (
	"github.com/thearchitectit/a2a-gateway/internal/agentcard"
	"github.com/thearchitectit/a2a-gateway/internal/task"
)

type RouteRequest struct {
	RequiredTags   []string         // skill tags the task needs
	RequiredCaps   []string         // capability names
	Metadata       map[string]string // hints for load balancing
	Task           *task.Task       // the task being routed
}

type RouteDecision struct {
	AgentCard  *agentcard.AgentCard
	Score      float64
	Reason     string
}

type Engine interface {
	Route(req RouteRequest) (*RouteDecision, error)
}
```

**File: `a2a/internal/routing/engine.go`**

```go
package routing

import (
	"errors"
	"sort"

	"github.com/thearchitectit/a2a-gateway/internal/agentcard"
)

var ErrNoAgentMatch = errors.New("no agent matches the required skills/capabilities")

type StaticEngine struct {
	registry *agentcard.Registry
	loadFn   func(agentID string) int // returns current task count
}

func NewStaticEngine(reg *agentcard.Registry, loadFn func(string) int) *StaticEngine {
	return &StaticEngine{registry: reg, loadFn: loadFn}
}

// Route selects the best agent for a request.
// Algorithm:
//  1. Filter agents whose skills cover ALL required tags.
//  2. Filter agents whose capabilities include ALL required caps.
//  3. Score remaining agents: base 1.0 + (matching tags * 0.1) - (load * 0.05).
//  4. Return highest-scoring agent.
func (e *StaticEngine) Route(req RouteRequest) (*RouteDecision, error) {
	all := e.registry.Snapshot()
	if len(all) == 0 {
		return nil, ErrNoAgentMatch
	}

	type scored struct {
		card  *agentcard.AgentCard
		score float64
		match int
		load  int
	}

	var candidates []scored
	for _, card := range all {
		// Check all required tags are present in at least one skill.
		matchedTags := 0
		allTagsCovered := true
		for _, reqTag := range req.RequiredTags {
			found := false
			for _, skill := range card.Skills {
				for _, t := range skill.Tags {
					if t == reqTag {
						found = true
						matchedTags++
						break
					}
				}
				if found {
					break
				}
			}
			if !found {
				allTagsCovered = false
				break
			}
		}
		if !allTagsCovered {
			continue
		}

		// Check capabilities.
		hasAllCaps := true
		for _, reqCap := range req.RequiredCaps {
			found := false
			for _, c := range card.Capabilities {
				if c.Name == reqCap {
					found = true
					break
				}
			}
			if !found {
				hasAllCaps = false
				break
			}
		}
		if !hasAllCaps {
			continue
		}

		load := 0
		if e.loadFn != nil {
			load = e.loadFn(card.AgentID)
		}
		score := 1.0 + float64(matchedTags)*0.1 - float64(load)*0.05
		candidates = append(candidates, scored{card: card, score: score, match: matchedTags, load: load})
	}

	if len(candidates) == 0 {
		return nil, ErrNoAgentMatch
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	best := candidates[0]
	return &RouteDecision{
		AgentCard: best.card,
		Score:     best.score,
		Reason:    "highest skill-match score minus load penalty",
	}, nil
}
```

Add the `Snapshot()` method to the registry:

```go
// a2a/internal/agentcard/registry.go (append)

// Snapshot returns a slice of all registered cards.
func (r *Registry) Snapshot() []*AgentCard {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*AgentCard, 0, len(r.cards))
	for _, c := range r.cards {
		out = append(out, c)
	}
	return out
}
```

**Tests:** `a2a/internal/routing/engine_test.go`:
- `TestRoute_ExactTagMatch` — single candidate with exact tags wins.
- `TestRoute_NoMatch` — returns `ErrNoAgentMatch`.
- `TestRoute_MultipleCandidates_BestScoreWins` — verifies scoring.
- `TestRoute_LoadPenalty` — loaded agent loses to unloaded with same tags.
- `TestRoute_RequiredCapabilityMissing` — agent excluded.
- `TestRoute_EmptyRegistry` — returns error.

---

### 5.4 Protocol Bindings

The gateway exposes three transport bindings. All three share the same `Manager` backend; they differ only in framing.

#### 5.4.1 JSON-RPC 2.0 over HTTP

**File: `a2a/internal/transport/http/jsonrpc.go`**

Implements the JSON-RPC 2.0 spec (https://www.jsonrpc.org/specification).

**Request format:**

```json
{
  "jsonrpc": "2.0",
  "id": "req-001",
  "method": "tasks/send",
  "params": {
    "agent_id": "alert-triage-bot",
    "message": {
      "message_id": "m-1",
      "role": "user",
      "parts": [{"kind": "text", "text": "Investigate CPU spike on host-42"}],
      "created_at": "2026-06-15T10:00:00Z"
    },
    "session_id": "sess-abc",
    "stream": false
  }
}
```

**Methods exposed:**

| JSON-RPC method         | Maps to                                |
|-------------------------|----------------------------------------|
| `tasks/send`            | `Manager.SendTask(ctx, req)`           |
| `tasks/get`             | `Manager.GetTask(ctx, taskID, historyLen)` |
| `tasks/cancel`          | `Manager.CancelTask(ctx, taskID)`      |
| `tasks/list`            | `Manager.ListTasks(ctx, filter, page)` |
| `tasks/subscribe`       | Stream of `Task` updates (SSE)         |
| `agents/register`       | `Registry.Register(ctx, card)`         |
| `agents/discover`       | `Registry.DiscoverBySkill` / `DiscoverByCapability` |

**Success response:**

```json
{
  "jsonrpc": "2.0",
  "id": "req-001",
  "result": {
    "id": "task-uuid-1234",
    "context_id": "ctx-uuid",
    "state": "submitted",
    "message": { ... },
    "artifacts": [],
    "created_at": "2026-06-15T10:00:00Z",
    "updated_at": "2026-06-15T10:00:00Z",
    "agent_id": "alert-triage-bot",
    "session_id": "sess-abc",
    "version": 1
  }
}
```

**Error response:**

```json
{
  "jsonrpc": "2.0",
  "id": "req-001",
  "error": {
    "code": -32602,
    "message": "Invalid params: agent_id is required",
    "data": {"field": "agent_id"}
  }
}
```

**Standard JSON-RPC error codes used:**

| Code   | Meaning         | A2A mapping                                    |
|--------|-----------------|------------------------------------------------|
| -32700 | Parse error     | Malformed JSON                                 |
| -32600 | Invalid request | Missing jsonrpc field, wrong version           |
| -32601 | Method not found| Unknown RPC method                             |
| -32602 | Invalid params  | Field validation failure                       |
| -32603 | Internal error  | Unexpected server error                        |
| -32000 | Server error    | Generic A2A server error                       |
| -32001 | Task not found  | `tasks/get` / `tasks/cancel` on missing ID     |
| -32002 | Invalid state   | Illegal state transition                       |
| -32003 | Agent not found | `agent_id` not registered                      |
| -32004 | Auth failed     | Bearer/mTLS/OAuth failure                      |
| -32005 | Rate limited    | Quota exceeded                                 |

**Implementation:**

```go
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
)

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (h *Handlers) handleJSONRPC(c echo.Context) error {
	var req jsonRPCRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, jsonRPCResponse{
			JSONRPC: "2.0",
			Error:   &jsonRPCError{Code: -32700, Message: "Parse error"},
		})
	}
	if req.JSONRPC != "2.0" {
		return c.JSON(http.StatusBadRequest, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonRPCError{Code: -32600, Message: "Invalid Request: jsonrpc must be 2.0"},
		})
	}
	if req.ID == nil {
		return c.JSON(http.StatusBadRequest, jsonRPCResponse{
			JSONRPC: "2.0",
			Error:   &jsonRPCError{Code: -32600, Message: "Invalid Request: id is required (use null for notifications, which are not supported)"},
		})
	}

	ctx := c.Request().Context()
	switch req.Method {
	case "tasks/send":
		return h.rpcSendTask(ctx, c, req)
	case "tasks/get":
		return h.rpcGetTask(ctx, c, req)
	case "tasks/cancel":
		return h.rpcCancelTask(ctx, c, req)
	case "tasks/list":
		return h.rpcListTasks(ctx, c, req)
	case "tasks/subscribe":
		return h.rpcSubscribeTask(ctx, c, req)
	case "agents/register":
		return h.rpcRegisterAgent(ctx, c, req)
	case "agents/discover":
		return h.rpcDiscoverAgents(ctx, c, req)
	default:
		return c.JSON(http.StatusOK, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonRPCError{Code: -32601, Message: "Method not found: " + req.Method},
		})
	}
}

// Each rpc* method: unmarshal Params, call Manager, wrap in jsonRPCResponse.
```

**Tests:** `a2a/internal/transport/http/jsonrpc_test.go`:
- `TestJSONRPC_SendTask_Success`
- `TestJSONRPC_SendTask_InvalidParams_Returns32602`
- `TestJSONRPC_GetTask_NotFound_Returns32001`
- `TestJSONRPC_CancelTask_AlreadyCompleted_Returns32002`
- `TestJSONRPC_UnknownMethod_Returns32601`
- `TestJSONRPC_MalformedJSON_Returns32700`
- `TestJSONRPC_WrongVersion_Returns32600`
- `TestJSONRPC_MissingID_Returns32600`
- `TestJSONRPC_BatchNotSupported_ReturnsSingleError` (batches return single error per spec)

#### 5.4.2 REST + JSON

**File: `a2a/internal/transport/http/rest.go`**

RESTful resource-oriented API parallel to the JSON-RPC surface.

| Method | Path                                | Description                  |
|--------|-------------------------------------|------------------------------|
| POST   | `/v1/tasks`                         | Create and dispatch task     |
| GET    | `/v1/tasks/{id}`                    | Retrieve task by ID          |
| DELETE | `/v1/tasks/{id}`                    | Cancel task                  |
| GET    | `/v1/tasks`                         | List tasks (paginated)       |
| GET    | `/v1/tasks/{id}/stream`             | SSE stream of task updates   |
| POST   | `/v1/agents`                        | Register agent card          |
| GET    | `/v1/agents`                        | List agent cards             |
| GET    | `/v1/agents/{id}`                   | Get single agent card        |
| DELETE | `/v1/agents/{id}`                   | Deregister agent             |
| GET    | `/.well-known/agent-card.json`      | Gateway self-description     |
| GET    | `/healthz`                          | Liveness probe               |
| GET    | `/readyz`                           | Readiness probe              |
| GET    | `/metrics`                          | Prometheus metrics           |

**Request/response examples (full JSON in section 7).**

**Tests:** `a2a/internal/transport/http/rest_test.go`:
- `TestREST_CreateTask_201`
- `TestREST_GetTask_200`
- `TestREST_GetTask_404`
- `TestREST_CancelTask_204`
- `TestREST_ListTasks_Pagination`
- `TestREST_RegisterAgent_201`
- `TestREST_DiscoverAgents_FilterBySkill`
- `TestREST_WellKnownAgentCard_200`
- `TestREST_Healthz_200`
- `TestREST_Readyz_DependsOnDB`

#### 5.4.3 gRPC server-streaming

**File: `a2a/internal/transport/grpc/server.go`**

Wraps the generated `A2AServiceServer` interface. The `SubscribeTask` method is server-streaming: the server sends one `Task` message per state change.

**Implementation:**

```go
package grpcx

import (
	"context"

	"github.com/thearchitectit/a2a-gateway/internal/pb"
	"github.com/thearchitectit/a2a-gateway/internal/task"
)

type Server struct {
	pb.UnimplementedA2AServiceServer
	manager *task.Manager
}

func NewServer(m *task.Manager) *Server {
	return &Server{manager: m}
}

func (s *Server) SendTask(ctx context.Context, req *pb.SendTaskRequest) (*pb.SendTaskResponse, error) {
	t, err := s.manager.SendTask(ctx, convertToManagerRequest(req))
	if err != nil {
		return nil, toGRPCError(err)
	}
	return &pb.SendTaskResponse{Task: convertToProto(t)}, nil
}

func (s *Server) SubscribeTask(req *pb.SubscribeTaskRequest, stream pb.A2AService_SubscribeTaskServer) error {
	ctx := stream.Context()
	ch, err := s.manager.Subscribe(ctx, req.TaskId)
	if err != nil {
		return toGRPCError(err)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case t, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(convertToProto(t)); err != nil {
				return err
			}
		}
	}
}
```

**gRPC status code mapping:**

| A2A error       | gRPC code           |
|-----------------|---------------------|
| TaskNotFound    | `NotFound` (5)      |
| InvalidState    | `FailedPrecondition`(9) |
| AgentNotFound   | `NotFound` (5)      |
| AuthFailed      | `Unauthenticated`(16) |
| RateLimited     | `ResourceExhausted`(8) |
| Internal        | `Internal` (13)     |

**Tests:** `a2a/internal/transport/grpc/server_test.go`:
- `TestGRPC_SendTask_Success`
- `TestGRPC_GetTask_NotFound`
- `TestGRPC_SubscribeTask_ReceivesUpdates` (uses in-memory subscriber)
- `TestGRPC_CancelTask_AlreadyCompleted`
- `TestGRPC_RegisterAgent_Persists`
- `TestGRPC_StreamBackpressure` (verifies slow consumer doesn't block publisher)

---

### 5.5 Task Persistence

**File: `a2a/internal/task/repository.go`**

PostgreSQL persistence using pgx v5. All methods take `context.Context` and return wrapped errors.

**Operations:**

| Method | SQL effect |
|--------|------------|
| `Create(ctx, *Task) error` | INSERT with version=1, return task with ID |
| `GetByID(ctx, id) (*Task, error)` | SELECT WHERE id=$1 |
| `ListByFilter(ctx, filter, limit, offset) ([]*Task, nextToken, error)` | SELECT with WHERE clauses |
| `UpdateState(ctx, id, from, to, version) error` | UPDATE ... WHERE id=$1 AND version=$2; on no-rows return `ErrConcurrentModification` |
| `AppendArtifact(ctx, taskID, *Artifact) error` | INSERT into task_artifacts |
| `AppendMessage(ctx, taskID, *Message) error` | INSERT into task_messages (history) |
| `ListArtifacts(ctx, taskID) ([]*Artifact, error)` | SELECT |

**Optimistic concurrency:**

```go
func (r *pgTaskRepo) UpdateState(ctx context.Context, id string, from, to task.TaskState, version int32) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Lock the row.
	var current task.TaskState
	var v int32
	err = tx.QueryRow(ctx, `SELECT state, version FROM tasks WHERE id=$1 FOR UPDATE`, id).Scan(&current, &v)
	if errors.Is(err, pgx.ErrNoRows) {
		return task.ErrNotFound
	}
	if err != nil {
		return err
	}
	if v != version {
		return task.ErrConcurrentModification
	}
	if err := task.ValidateTransition(current, to); err != nil {
		return err
	}

	now := time.Now().UTC()
	completedAt := nullableTime(nil)
	if to.Terminal() {
		completedAt = nullableTime(&now)
	}

	_, err = tx.Exec(ctx, `
		UPDATE tasks SET state=$1, version=version+1, updated_at=$2, completed_at=$3
		WHERE id=$4 AND version=$5
	`, to, now, completedAt, id, version)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
```

**File: `a2a/internal/task/manager.go`**

Orchestrates business logic on top of the repository:

```go
type Manager struct {
	repo     Repository
	router   routing.Engine
	notifier *pushnotify.Dispatcher
	subs     *SubscriberHub  // in-process pub/sub
	logger   *slog.Logger
}

func NewManager(repo Repository, router routing.Engine, notifier *pushnotify.Dispatcher, subs *SubscriberHub, logger *slog.Logger) *Manager {
	return &Manager{repo: repo, router: router, notifier: notifier, subs: subs, logger: logger}
}

// SendTask: validate, route, persist, dispatch to agent, notify subscribers.
func (m *Manager) SendTask(ctx context.Context, req SendTaskRequest) (*task.Task, error) {
	if req.AgentID == "" && len(req.RequiredTags) == 0 {
		return nil, errors.New("either agent_id or required_tags must be provided")
	}

	var agentID string
	if req.AgentID != "" {
		agentID = req.AgentID
	} else {
		dec, err := m.router.Route(routing.RouteRequest{
			RequiredTags: req.RequiredTags,
			RequiredCaps: req.RequiredCaps,
		})
		if err != nil {
			return nil, err
		}
		agentID = dec.AgentCard.AgentID
	}

	t := &task.Task{
		ID:        uuid.NewString(),
		ContextID: req.ContextID,
		State:     task.TaskStateSubmitted,
		Message:   req.Message,
		AgentID:   agentID,
		SessionID: req.SessionID,
		Metadata:  req.Metadata,
		Version:   1,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	if err := m.repo.Create(ctx, t); err != nil {
		return nil, err
	}

	m.subs.Publish(t)
	m.notifier.NotifyTaskCreated(ctx, t)

	// Transition to working immediately (fire-and-forget).
	go m.transitionToWorking(t)

	return t, nil
}
```

**Tests:** `a2a/internal/task/manager_test.go`:
- `TestSendTask_RoutesToAgent`
- `TestSendTask_NoRoute_ReturnsError`
- `TestGetTask_IncludesHistory`
- `TestCancelTask_OnlyFromNonTerminal`
- `TestCancelTask_TerminalState_ReturnsError`
- `TestSubscribeTask_ReceivesCreatedAndCompleted`
- `TestOptimisticConcurrency_DoubleUpdate_OneWins`

**Tests:** `a2a/internal/task/repository_test.go`:
- `TestCreate_GeneratesIDAndVersion`
- `TestGetByID_NotFound`
- `TestListByFilter_Pagination`
- `TestUpdateState_ConcurrencyConflict`
- `TestAppendArtifact_Ordered`

---

### 5.6 Push Notification System

**Purpose:** Deliver task state changes to external webhook subscribers with HMAC signing and retry.

**File: `a2a/internal/pushnotify/types.go`**

```go
package pushnotify

import (
	"time"
)

type WebhookConfig struct {
	ID            string    `json:"id"`
	TaskID        string    `json:"task_id"`
	URL           string    `json:"url"`
	Secret        string    `json:"secret"`        // HMAC key
	Events        []string  `json:"events"`        // ["task.created", "task.completed", ...]
	MaxRetries    int       `json:"max_retries"`
	TimeoutSec    int       `json:"timeout_sec"`
	Active        bool      `json:"active"`
	CreatedAt     time.Time `json:"created_at"`
}

type Delivery struct {
	ID            string     `json:"id"`
	WebhookID     string     `json:"webhook_id"`
	TaskID        string     `json:"task_id"`
	Event         string     `json:"event"`
	Payload       []byte     `json:"payload"`
	StatusCode    int        `json:"status_code"`
	ResponseBody  string     `json:"response_body"`
	Attempt       int        `json:"attempt"`
	DeliveredAt   *time.Time `json:"delivered_at,omitempty"`
	NextRetryAt   *time.Time `json:"next_retry_at,omitempty"`
	Failed        bool       `json:"failed"`
}
```

**File: `a2a/internal/pushnotify/signing.go`**

```go
package pushnotify

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// Sign returns the hex HMAC-SHA256 of body using secret.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
```

**File: `a2a/internal/pushnotify/retry.go`**

Exponential backoff: `delay = base * 2^attempt`, jittered by ±20%. Max delay 5 minutes.

```go
func NextDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	base := 5 * time.Second
	maxD := 5 * time.Minute
	d := base * (1 << attempt)
	if d > maxD {
		d = maxD
	}
	jitter := time.Duration(rand.Int63n(int64(d) / 5))  // ±20%
	if rand.Intn(2) == 0 {
		d += jitter
	} else {
		d -= jitter
	}
	return d
}
```

**File: `a2a/internal/pushnotify/dispatcher.go`**

```go
type Dispatcher struct {
	repo  WebhookRepository
	http  *http.Client
	pool  *pgxpool.Pool
	queue chan deliveryJob
	stop  chan struct{}
}

type deliveryJob struct {
	webhook WebhookConfig
	event   string
	payload []byte
	attempt int
}

func NewDispatcher(repo WebhookRepository, pool *pgxpool.Pool) *Dispatcher {
	d := &Dispatcher{
		repo: repo,
		http: &http.Client{Timeout: 10 * time.Second},
		pool: pool,
		queue: make(chan deliveryJob, 1024),
		stop: make(chan struct{}),
	}
	for i := 0; i < 4; i++ {
		go d.worker()
	}
	return d
}

// EnqueueTaskEvent serializes the event payload and enqueues for delivery.
func (d *Dispatcher) EnqueueTaskEvent(ctx context.Context, taskID, event string, body any) {
	webhooks, err := d.repo.ListActiveForTask(ctx, taskID, event)
	if err != nil {
		return
	}
	payload, _ := json.Marshal(body)
	for _, w := range webhooks {
		select {
		case d.queue <- deliveryJob{webhook: w, event: event, payload: payload, attempt: 0}:
		default:
			// Queue full; log and drop.
		}
	}
}

func (d *Dispatcher) worker() {
	for {
		select {
		case <-d.stop:
			return
		case job := <-d.queue:
			d.deliver(job)
		}
	}
}

func (d *Dispatcher) deliver(job deliveryJob) {
	req, _ := http.NewRequest("POST", job.webhook.URL, bytes.NewReader(job.payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-A2A-Event", job.event)
	req.Header.Set("X-A2A-Signature", Sign(job.webhook.Secret, job.payload))
	req.Header.Set("X-A2A-Delivery", uuid.NewString())
	req.Header.Set("X-A2A-Attempt", strconv.Itoa(job.attempt+1))

	resp, err := d.http.Do(req)
	if err == nil && resp.StatusCode < 300 {
		d.repo.MarkDelivered(context.Background(), job.webhook.ID, job.event, resp.StatusCode, "")
		resp.Body.Close()
		return
	}
	if resp != nil {
		resp.Body.Close()
	}
	if job.attempt+1 >= job.webhook.MaxRetries {
		d.repo.MarkFailed(context.Background(), job.webhook.ID, job.event, job.attempt+1)
		return
	}
	delay := NextDelay(job.attempt)
	d.repo.ScheduleRetry(context.Background(), job.webhook.ID, job.event, job.attempt+1, time.Now().Add(delay))
	time.AfterFunc(delay, func() {
		d.queue <- deliveryJob{
			webhook: job.webhook,
			event:   job.event,
			payload: job.payload,
			attempt: job.attempt + 1,
		}
	})
}
```

**Webhook events:**

| Event              | Fired when                                    |
|--------------------|-----------------------------------------------|
| `task.created`     | Task persisted in `submitted` state           |
| `task.working`     | Task enters `working`                         |
| `task.input_required` | Task enters `input-required`              |
| `task.completed`   | Task enters `completed`                       |
| `task.failed`      | Task enters `failed`                          |
| `task.canceled`    | Task enters `canceled`                        |
| `artifact.created` | New artifact appended                         |

**Signature verification example for consumers:**

```python
expected = "sha256=" + hmac.new(secret.encode(), body, sha256).hexdigest()
if not hmac.compare_digest(expected, request.headers["X-A2A-Signature"]):
    return 401
```

**Tests:** `a2a/internal/pushnotify/dispatcher_test.go`:
- `TestSign_Deterministic`
- `TestSign_DifferentSecrets_DifferentSignatures`
- `TestNextDelay_Exponential`
- `TestNextDelay_Capped`
- `TestDeliver_2xx_MarksDelivered`
- `TestDeliver_5xx_Retries`
- `TestDeliver_MaxRetries_MarksFailed`
- `TestDeliver_SignatureHeaderPresent`
- `TestQueueFull_Drops`

---

### 5.7 Authentication & Authorization

**Purpose:** Pluggable auth supporting bearer, mTLS, and OAuth 2.1.

**File: `a2a/internal/auth/interface.go`**

```go
package auth

import "context"

type Principal struct {
	Subject  string
	Scopes   []string
	AgentID  string
	Metadata map[string]string
}

type Authenticator interface {
	Authenticate(ctx context.Context, r *AuthRequest) (*Principal, error)
}

type AuthRequest struct {
	Headers    http.Header
	TLS        *tls.ConnectionState  // for mTLS
	RemoteAddr string
}
```

**File: `a2a/internal/auth/bearer.go`**

Static bearer token validation against an in-memory list or PostgreSQL `api_keys` table.

```go
type BearerAuth struct {
	keys map[string]*Principal // token -> principal
}

func NewBearerAuth(keys map[string]*Principal) *BearerAuth {
	return &BearerAuth{keys: keys}
}

func (b *BearerAuth) Authenticate(ctx context.Context, req *AuthRequest) (*Principal, error) {
	h := req.Headers.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return nil, ErrMissingBearer
	}
	token := strings.TrimPrefix(h, "Bearer ")
	p, ok := b.keys[token]
	if !ok {
		return nil, ErrInvalidToken
	}
	return p, nil
}
```

**File: `a2a/internal/auth/mtls.go`**

Verifies the client certificate and maps `CN` or `SAN` to a principal.

```go
type MTLSAuth struct {
	certToPrincipal map[string]*Principal // fingerprint (SHA-256) -> principal
}

func NewMTLSAuth(certs map[string]*Principal) *MTLSAuth {
	return &MTLSAuth{certToPrincipal: certs}
}

func (m *MTLSAuth) Authenticate(ctx context.Context, req *AuthRequest) (*Principal, error) {
	if req.TLS == nil || len(req.TLS.PeerCertificates) == 0 {
		return nil, ErrNoClientCert
	}
	cert := req.TLS.PeerCertificates[0]
	fp := sha256.Sum256(cert.Raw)
	fingerprint := hex.EncodeToString(fp[:])
	p, ok := m.certToPrincipal[fingerprint]
	if !ok {
		return nil, ErrUnknownCert
	}
	return p, nil
}
```

**File: `a2a/internal/auth/oauth.go`**

OAuth 2.1 token introspection per RFC 7662.

```go
type OAuthAuth struct {
	introspectURL string
	clientID      string
	clientSecret  string
	cache         *ttlcache.Cache[string, *Principal]
}

func (o *OAuthAuth) Authenticate(ctx context.Context, req *AuthRequest) (*Principal, error) {
	token := extractBearer(req.Headers)
	if token == "" {
		return nil, ErrMissingBearer
	}
	if p, ok := o.cache.Get(token); ok {
		return p, nil
	}
	body, _ := json.Marshal(map[string]string{
		"token": token,
		"token_type_hint": "access_token",
	})
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", o.introspectURL, bytes.NewReader(body))
	httpReq.SetBasicAuth(o.clientID, o.clientSecret)
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil || resp.StatusCode != 200 {
		return nil, ErrIntrospectionFailed
	}
	defer resp.Body.Close()
	var ir struct {
		Active   bool   `json:"active"`
		Sub      string `json:"sub"`
		Scope    string `json:"scope"`
		ClientID string `json:"client_id"`
		Exp      int64  `json:"exp"`
	}
	json.NewDecoder(resp.Body).Decode(&ir)
	if !ir.Active {
		return nil, ErrInactiveToken
	}
	p := &Principal{Subject: ir.Sub, Scopes: strings.Fields(ir.Scope), AgentID: ir.ClientID}
	o.cache.Set(token, p, time.Until(time.Unix(ir.Exp, 0)))
	return p, nil
}
```

**File: `a2a/internal/transport/http/middleware.go`**

```go
func AuthMiddleware(auths []auth.Authenticator) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := &auth.AuthRequest{
				Headers: c.Request().Header,
			}
			if c.Request().TLS != nil {
				req.TLS = c.Request().TLS
			}
			req.RemoteAddr = c.RealIP()
			for _, a := range auths {
				p, err := a.Authenticate(c.Request().Context(), req)
				if err == nil {
					c.Set("principal", p)
					return next(c)
				}
			}
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}
	}
}
```

**Authorization scopes:**

| Scope                  | Permits                                          |
|------------------------|--------------------------------------------------|
| `tasks:read`           | `tasks/get`, `tasks/list`                        |
| `tasks:write`          | `tasks/send`, `tasks/cancel`                     |
| `tasks:stream`         | `tasks/subscribe` (SSE / gRPC stream)            |
| `agents:read`          | `agents/discover`                                |
| `agents:write`         | `agents/register`                                |
| `webhooks:write`       | Webhook subscription CRUD                        |

**Tests:**
- `a2a/internal/auth/bearer_test.go`: `TestBearer_ValidToken`, `TestBearer_MissingHeader`, `TestBearer_InvalidToken`, `TestBearer_WrongScheme`
- `a2a/internal/auth/mtls_test.go`: `TestMTLS_ValidCert`, `TestMTLS_NoClientCert`, `TestMTLS_UnknownFingerprint`
- `a2a/internal/auth/oauth_test.go`: `TestOAuth_ActiveToken`, `TestOAuth_InactiveToken`, `TestOAuth_IntrospectionFailure`, `TestOAuth_CacheHit`

---

### 5.8 Event-to-Task Bridge

**Purpose:** Convert internal RMM events (alerts, tickets, deploys, scans) into A2A Tasks routed to appropriate agents.

**File: `a2a/internal/bridge/mapping.go`**

```go
package bridge

import "github.com/thearchitectit/a2a-gateway/internal/task"

// EventType is the kind of RMM event arriving on the bus.
type EventType string

const (
	EventAlertCreated   EventType = "alert.created"
	EventAlertResolved  EventType = "alert.resolved"
	EventTicketOpened   EventType = "ticket.opened"
	EventTicketClosed   EventType = "ticket.closed"
	EventDeployStarted  EventType = "deploy.started"
	EventDeployFinished EventType = "deploy.finished"
	EventScanCompleted  EventType = "scan.completed"
	EventPatchAvailable EventType = "patch.available"
)

// MapEventToTaskRequest converts a raw RMM event into a task.SendTaskRequest.
func MapEventToTaskRequest(evt RMMEvent) (task.SendTaskRequest, error) {
	switch evt.Type {
	case EventAlertCreated:
		return alertToTask(evt)
	case EventTicketOpened:
		return ticketToTask(evt)
	case EventDeployStarted:
		return deployToTask(evt)
	case EventScanCompleted:
		return scanToTask(evt)
	case EventPatchAvailable:
		return patchToTask(evt)
	default:
		return task.SendTaskRequest{}, ErrUnsupportedEvent
	}
}
```

**File: `a2a/internal/bridge/handlers.go`**

Each handler builds a `SendTaskRequest` with appropriate `RequiredTags` so the router picks the right agent:

```go
func alertToTask(evt RMMEvent) (task.SendTaskRequest, error) {
	return task.SendTaskRequest{
		RequiredTags: []string{"alert.triage", "monitoring"},
		RequiredCaps: []string{"incident-response"},
		Message: task.Message{
			MessageID: uuid.NewString(),
			Role:      "user",
			Parts: []task.Part{
				{Kind: "text", Text: fmt.Sprintf("Alert: %s on %s (severity=%s)", evt.Data["title"], evt.Data["host"], evt.Data["severity"])},
				{Kind: "data", Data: json.RawMessage(evt.Data["raw"])},
			},
			CreatedAt: time.Now().UTC(),
		},
		Metadata: map[string]string{"source_event": string(evt.Type), "event_id": evt.ID},
	}, nil
}
```

**File: `a2a/internal/bridge/event_bridge.go`**

Consumes from an internal Go channel (fed by the RMM event bus) and calls `Manager.SendTask`:

```go
type EventBridge struct {
	manager *task.Manager
	events  <-chan RMMEvent
	logger  *slog.Logger
}

func (b *EventBridge) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-b.events:
			if !ok {
				return
			}
			req, err := MapEventToTaskRequest(evt)
			if err != nil {
				b.logger.Warn("unsupported event", "type", evt.Type, "err", err)
				continue
			}
			if _, err := b.manager.SendTask(ctx, req); err != nil {
				b.logger.Error("send task from event", "event_id", evt.ID, "err", err)
			}
		}
	}
}
```

**Event-to-tag mapping table:**

| Event               | Required tags                            | Required capabilities    |
|---------------------|------------------------------------------|--------------------------|
| `alert.created`     | `alert.triage`, `monitoring`             | `incident-response`      |
| `alert.resolved`    | `alert.closure`                          | `incident-response`      |
| `ticket.opened`     | `ticket.intake`, `helpdesk`              | `ticketing`              |
| `ticket.closed`     | `ticket.closure`                         | `ticketing`              |
| `deploy.started`    | `deploy.execution`, `change-management`  | `deployment`             |
| `deploy.finished`   | `deploy.verification`                    | `deployment`             |
| `scan.completed`    | `vulnerability.triage`                   | `security-scanning`      |
| `patch.available`   | `patch.planning`                         | `patch-management`       |

**Tests:** `a2a/internal/bridge/event_bridge_test.go`:
- `TestAlertCreated_GeneratesTask`
- `TestTicketOpened_GeneratesTask`
- `TestUnsupportedEvent_Skipped`
- `TestMapping_AllEventTypes_ProduceValidRequests`
- `TestBridge_RoutesThroughManager`

---

### 5.9 Streaming Support

**Purpose:** Real-time delivery of task state changes to subscribers.

**File: `a2a/internal/task/subscribers.go`** (in task package)

```go
type SubscriberHub struct {
	mu   sync.RWMutex
	subs map[string]map[chan *task.Task]struct{} // taskID -> set of channels
}

func NewSubscriberHub() *SubscriberHub {
	return &SubscriberHub{subs: make(map[string]map[chan *task.Task]struct{})}
}

func (h *SubscriberHub) Subscribe(taskID string) (<-chan *task.Task, func()) {
	ch := make(chan *task.Task, 16)
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[taskID]; !ok {
		h.subs[taskID] = make(map[chan *task.Task]struct{})
	}
	h.subs[taskID][ch] = struct{}{}
	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if set, ok := h.subs[taskID]; ok {
			delete(set, ch)
			if len(set) == 0 {
				delete(h.subs, taskID)
			}
		}
		close(ch)
	}
	return ch, cancel
}

func (h *SubscriberHub) Publish(t *task.Task) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	set, ok := h.subs[t.ID]
	if !ok {
		return
	}
	for ch := range set {
		select {
		case ch <- t:
		default:
			// subscriber too slow; drop and disconnect
			delete(set, ch)
			close(ch)
		}
	}
}
```

**SSE transport (REST binding):**

```go
// a2a/internal/transport/http/sse.go

func (h *Handlers) handleTaskStream(c echo.Context) error {
	taskID := c.Param("id")
	ch, cancel := h.subs.Subscribe(taskID)
	defer cancel()

	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().Header().Set("X-Accel-Buffering", "no")
	c.Response().WriteHeader(http.StatusOK)
	flusher, ok := c.Response().Writer.(http.Flusher)
	if !ok {
		return echo.NewHTTPError(http.StatusInternalServerError, "streaming unsupported")
	}

	// Send heartbeat every 15s to keep connection alive.
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.Request().Context().Done():
			return nil
		case t, ok := <-ch:
			if !ok {
				fmt.Fprintf(c.Response().Writer, "event: end\ndata: {}\n\n")
				flusher.Flush()
				return nil
			}
			payload, _ := json.Marshal(t)
			fmt.Fprintf(c.Response().Writer, "event: task\ndata: %s\n\n", payload)
			flusher.Flush()
			if t.State.Terminal() {
				return nil
			}
		case <-ticker.C:
			fmt.Fprintf(c.Response().Writer, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}
```

**Tests:** `a2a/internal/transport/http/sse_test.go`:
- `TestSSE_ReceivesStateTransitions`
- `TestSSE_TerminalTaskClosesStream`
- `TestSSE_HeartbeatKeepsConnection`
- `TestSSE_ClientDisconnectCleansUp`

---

### 5.10 Error Handling

**File: `a2a/internal/errors/codes.go`**

```go
package a2aerr

// Standard JSON-RPC error codes.
const (
	CodeParseError        = -32700
	CodeInvalidRequest    = -32600
	CodeMethodNotFound    = -32601
	CodeInvalidParams     = -32602
	CodeInternal          = -32603
	CodeServerError       = -32000
	CodeTaskNotFound      = -32001
	CodeInvalidState      = -32002
	CodeAgentNotFound     = -32003
	CodeAuthFailed        = -32004
	CodeRateLimited       = -32005
	CodeWebhookDelivery   = -32006
	CodeProtocolViolation = -32007
)
```

**File: `a2a/internal/errors/errors.go`**

```go
type Error struct {
	Code    int
	Message string
	Data    interface{}
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("a2a error %d: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("a2a error %d: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

func New(code int, msg string) *Error {
	return &Error{Code: code, Message: msg}
}

func Wrap(code int, msg string, cause error) *Error {
	return &Error{Code: code, Message: msg, Cause: cause}
}
```

**Handler pattern:**

```go
func toJSONRPCError(err error) *jsonRPCError {
	var a2e *a2aerr.Error
	if errors.As(err, &a2e) {
		return &jsonRPCError{Code: a2e.Code, Message: a2e.Message, Data: a2e.Data}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return &jsonRPCError{Code: a2aerr.CodeTaskNotFound, Message: "not found"}
	}
	return &jsonRPCError{Code: a2aerr.CodeInternal, Message: "internal server error"}
}
```

**Error response format (all bindings):**

```json
{
  "code": -32002,
  "message": "Illegal transition: completed -> working",
  "data": {
    "task_id": "task-uuid-1234",
    "from": "completed",
    "to": "working"
  }
}
```

**Tests:** `a2a/internal/errors/errors_test.go`:
- `TestError_Error_IncludesCause`
- `TestError_Unwrap_ReturnsCause`
- `TestNew_DoesNotSetCause`
- `TestWrap_SetsCause`
- `TestToJSONRPCError_KnownTypes`

---

### 5.11 Gateway Scaling

**Purpose:** Run multiple stateless gateway instances behind a load balancer.

**Strategy:** All gateway instances are stateless. State lives in PostgreSQL. In-process pub/sub (`SubscriberHub`) is used only for fan-out within a single instance; cross-instance streaming requires Redis pub/sub (optional, behind a feature flag).

**File: `a2a/internal/scaling/shutdown.go`**

Graceful shutdown:

```go
func GracefulShutdown(srv *http.Server, grpcSrv *grpc.Server, timeout time.Duration, logger *slog.Logger) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	logger.Info("shutdown signal received", "signal", sig.String())

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("http shutdown error", "err", err)
	}
	grpcSrv.GracefulStop()
	logger.Info("shutdown complete")
}
```

**File: `a2a/internal/scaling/leader.go`** (optional)

Leader election using PostgreSQL advisory locks for any future scheduled jobs. Not required for the core A2A surface.

**Horizontal scaling notes:**

- Gateway is stateless; scale by adding replicas.
- PostgreSQL connection pool sized at `max(10, 2 * CPU)`.
- Use PgBouncer in transaction-pooling mode in front of PostgreSQL.
- Health checks: `/healthz` returns 200 always; `/readyz` returns 200 only when DB reachable and registry loaded.
- Deploy with `HPA` targeting 70% CPU, min 2, max 20 replicas.

**File: `deploy/hpa.yaml`**

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: a2a-gateway
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: a2a-gateway
  minReplicas: 2
  maxReplicas: 20
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
  - type: Resource
    resource:
      name: memory
      target:
        type: Utilization
        averageUtilization: 80
```

---

### 5.12 A2A Test Suite

The test suite has four layers: unit, integration, contract, and load.

**Unit tests:** Co-located `*_test.go` next to each source file (Go convention).

**Integration tests:** `a2a/tests/integration/*.go` — start a real test database, real server, exercise full request flow.

**File: `a2a/tests/helpers/testdb.go`**

```go
func StartTestDB(t *testing.T) (*pgxpool.Pool, func()) {
	// Use testcontainers-go to spin up postgres:16.
	// Run all migrations.
	// Return pool + cleanup.
}
```

**File: `a2a/tests/helpers/testserver.go`**

```go
func StartTestServer(t *testing.T, pool *pgxpool.Pool) (string, func()) {
	// Start Echo + gRPC on random ports.
	// Return base URL + cleanup.
}
```

**Contract tests:** Verify JSON shapes against golden files.

**Load tests:** k6 scripts in `a2a/tests/load/`.

**File: `a2a/tests/load/k6_throughput.js`**

```javascript
import http from 'k6/http';
import { check } from 'k6';
export const options = {
  stages: [
    { duration: '30s', target: 100 },
    { duration: '1m', target: 500 },
    { duration: '30s', target: 0 },
  ],
  thresholds: {
    http_req_duration: ['p(95)<200'],
    http_req_failed: ['rate<0.01'],
  },
};
export default function () {
  const res = http.post(__ENV.BASE_URL + '/v1/tasks', JSON.stringify({
    agent_id: 'echo-agent',
    message: { message_id: 'm-' + __VU, role: 'user', parts: [{kind:'text', text:'ping'}] },
  }), { headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + __ENV.TOKEN } });
  check(res, { 'status is 201': (r) => r.status === 201 });
}
```

**Test inventory:**

| Test file | Verifies |
|-----------|----------|
| `task_lifecycle_test.go` | Full lifecycle: create → work → complete; cancel mid-flight |
| `agent_card_test.go` | Register, discover, update, deregister |
| `routing_test.go` | Skill match, load balancing, no-match |
| `jsonrpc_test.go` | All JSON-RPC methods, error codes |
| `rest_test.go` | All REST endpoints, pagination |
| `sse_test.go` | Stream end-to-end |
| `grpc_test.go` | gRPC unary + streaming |
| `push_notify_test.go` | Webhook delivery, retry, signing |
| `event_bridge_test.go` | Each event type maps to correct task |
| `auth_test.go` | Bearer, mTLS, OAuth paths |
| `error_handling_test.go` | All error codes mapped correctly |
| `jsonrpc_schema_test.go` | Request/response JSON Schema validation |
| `a2a_proto_compliance_test.go` | Proto round-trip for all messages |

---

## 6. Data Models (SQL DDL)

### File: `a2a/migrations/001_create_tasks.up.sql`

```sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS tasks (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    context_id    UUID NOT NULL,
    state         VARCHAR(32) NOT NULL CHECK (state IN (
        'submitted','working','input-required','completed','failed','canceled'
    )),
    agent_id      VARCHAR(255) NOT NULL,
    session_id    VARCHAR(255),
    message       JSONB NOT NULL,
    metadata      JSONB NOT NULL DEFAULT '{}',
    version       INTEGER NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at  TIMESTAMPTZ
);

COMMENT ON TABLE tasks IS 'A2A task lifecycle records';
COMMENT ON COLUMN tasks.version IS 'Optimistic concurrency token, incremented on every update';
```

### File: `a2a/migrations/001_create_tasks.down.sql`

```sql
DROP TABLE IF EXISTS tasks;
```

### File: `a2a/migrations/002_create_artifacts.up.sql`

```sql
CREATE TABLE IF NOT EXISTS task_artifacts (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id       UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    artifact_id   VARCHAR(255) NOT NULL,
    name          VARCHAR(500) NOT NULL,
    description   TEXT,
    parts         JSONB NOT NULL,
    metadata      JSONB NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (task_id, artifact_id)
);

CREATE TABLE IF NOT EXISTS task_messages (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id       UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    message_id    VARCHAR(255) NOT NULL,
    role          VARCHAR(32) NOT NULL CHECK (role IN ('user','agent','system')),
    parts         JSONB NOT NULL,
    metadata      JSONB NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (task_id, message_id)
);
```

### File: `a2a/migrations/002_create_artifacts.down.sql`

```sql
DROP TABLE IF EXISTS task_messages;
DROP TABLE IF EXISTS task_artifacts;
```

### File: `a2a/migrations/003_create_agent_cards.up.sql`

```sql
CREATE TABLE IF NOT EXISTS agent_cards (
    agent_id             VARCHAR(255) PRIMARY KEY,
    name                 VARCHAR(500) NOT NULL,
    description          TEXT NOT NULL,
    url                  TEXT NOT NULL,
    version              VARCHAR(64) NOT NULL,
    provider             VARCHAR(255) NOT NULL,
    documentation        TEXT,
    skills               JSONB NOT NULL DEFAULT '[]',
    capabilities         JSONB NOT NULL DEFAULT '[]',
    default_input_modes  JSONB NOT NULL DEFAULT '[]',
    default_output_modes JSONB NOT NULL DEFAULT '[]',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE agent_cards IS 'Agent capability discovery metadata';
```

### File: `a2a/migrations/003_create_agent_cards.down.sql`

```sql
DROP TABLE IF EXISTS agent_cards;
```

### File: `a2a/migrations/004_create_push_webhooks.up.sql`

```sql
CREATE TABLE IF NOT EXISTS push_webhooks (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id      UUID REFERENCES tasks(id) ON DELETE CASCADE,
    url          TEXT NOT NULL,
    secret       VARCHAR(128) NOT NULL,
    events       JSONB NOT NULL DEFAULT '[]',
    max_retries  INTEGER NOT NULL DEFAULT 5,
    timeout_sec  INTEGER NOT NULL DEFAULT 10,
    active       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS push_deliveries (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_id     UUID NOT NULL REFERENCES push_webhooks(id) ON DELETE CASCADE,
    task_id        UUID NOT NULL,
    event          VARCHAR(64) NOT NULL,
    payload        JSONB NOT NULL,
    status_code    INTEGER,
    response_body  TEXT,
    attempt        INTEGER NOT NULL DEFAULT 0,
    delivered_at   TIMESTAMPTZ,
    next_retry_at  TIMESTAMPTZ,
    failed         BOOLEAN NOT NULL DEFAULT FALSE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### File: `a2a/migrations/004_create_push_webhooks.down.sql`

```sql
DROP TABLE IF EXISTS push_deliveries;
DROP TABLE IF EXISTS push_webhooks;
```

### File: `a2a/migrations/005_create_events_inbox.up.sql`

```sql
CREATE TABLE IF NOT EXISTS events_inbox (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type    VARCHAR(64) NOT NULL,
    source        VARCHAR(128) NOT NULL,
    payload       JSONB NOT NULL,
    received_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at  TIMESTAMPTZ,
    task_id       UUID REFERENCES tasks(id) ON DELETE SET NULL,
    error         TEXT
);
```

### File: `a2a/migrations/005_create_events_inbox.down.sql`

```sql
DROP TABLE IF EXISTS events_inbox;
```

### File: `a2a/migrations/006_create_indexes.up.sql`

```sql
CREATE INDEX IF NOT EXISTS idx_tasks_agent_id ON tasks(agent_id);
CREATE INDEX IF NOT EXISTS idx_tasks_session_id ON tasks(session_id);
CREATE INDEX IF NOT EXISTS idx_tasks_state ON tasks(state);
CREATE INDEX IF NOT EXISTS idx_tasks_context_id ON tasks(context_id);
CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_state_created ON tasks(state, created_at DESC) WHERE state IN ('submitted','working');

CREATE INDEX IF NOT EXISTS idx_task_artifacts_task_id ON task_artifacts(task_id);
CREATE INDEX IF NOT EXISTS idx_task_messages_task_id_created ON task_messages(task_id, created_at);

CREATE INDEX IF NOT EXISTS idx_agent_cards_name ON agent_cards(name);

CREATE INDEX IF NOT EXISTS idx_push_webhooks_task_id ON push_webhooks(task_id) WHERE active = TRUE;
CREATE INDEX IF NOT EXISTS idx_push_deliveries_webhook_id ON push_deliveries(webhook_id);
CREATE INDEX IF NOT EXISTS idx_push_deliveries_next_retry ON push_deliveries(next_retry_at) WHERE next_retry_at IS NOT NULL AND failed = FALSE;

CREATE INDEX IF NOT EXISTS idx_events_inbox_unprocessed ON events_inbox(received_at) WHERE processed_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_events_inbox_type ON events_inbox(event_type, received_at DESC);
```

### File: `a2a/migrations/006_create_indexes.down.sql`

```sql
DROP INDEX IF EXISTS idx_events_inbox_type;
DROP INDEX IF EXISTS idx_events_inbox_unprocessed;
DROP INDEX IF EXISTS idx_push_deliveries_next_retry;
DROP INDEX IF EXISTS idx_push_deliveries_webhook_id;
DROP INDEX IF EXISTS idx_push_webhooks_task_id;
DROP INDEX IF EXISTS idx_agent_cards_name;
DROP INDEX IF EXISTS idx_task_messages_task_id_created;
DROP INDEX IF EXISTS idx_task_artifacts_task_id;
DROP INDEX IF EXISTS idx_tasks_state_created;
DROP INDEX IF EXISTS idx_tasks_created_at;
DROP INDEX IF EXISTS idx_tasks_context_id;
DROP INDEX IF EXISTS idx_tasks_state;
DROP INDEX IF EXISTS idx_tasks_session_id;
DROP INDEX IF EXISTS idx_tasks_agent_id;
```

### File: `a2a/migrations/verify_schema.sql`

```sql
-- Verify all required A2A tables exist.
SELECT 'tasks' AS table_name, EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='tasks') AS exists
UNION ALL SELECT 'task_artifacts', EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='task_artifacts')
UNION ALL SELECT 'task_messages', EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='task_messages')
UNION ALL SELECT 'agent_cards', EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='agent_cards')
UNION ALL SELECT 'push_webhooks', EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='push_webhooks')
UNION ALL SELECT 'push_deliveries', EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='push_deliveries')
UNION ALL SELECT 'events_inbox', EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='events_inbox');
```

---

## 7. API Schemas (Full JSON)

### 7.1 REST: POST /v1/tasks

**Request:**

```json
{
  "agent_id": "alert-triage-bot",
  "required_tags": ["alert.triage"],
  "required_capabilities": ["incident-response"],
  "context_id": "ctx-9f8e7d6c",
  "session_id": "sess-abc",
  "stream": false,
  "message": {
    "message_id": "m-001",
    "role": "user",
    "parts": [
      { "kind": "text", "text": "Investigate CPU spike on host-42", "mime_type": "text/plain" },
      { "kind": "data", "data": { "host": "host-42", "metric": "cpu_percent", "value": 95 } }
    ],
    "metadata": { "source": "grafana" }
  },
  "metadata": { "priority": "high" }
}
```

**Response 201:**

```json
{
  "id": "task-uuid-1234",
  "context_id": "ctx-9f8e7d6c",
  "state": "submitted",
  "agent_id": "alert-triage-bot",
  "session_id": "sess-abc",
  "message": {
    "message_id": "m-001",
    "role": "user",
    "parts": [ ... ],
    "created_at": "2026-06-15T10:00:00Z"
  },
  "artifacts": [],
  "created_at": "2026-06-15T10:00:00Z",
  "updated_at": "2026-06-15T10:00:00Z",
  "version": 1
}
```

### 7.2 REST: GET /v1/tasks/{id}

**Response 200:**

```json
{
  "id": "task-uuid-1234",
  "context_id": "ctx-9f8e7d6c",
  "state": "working",
  "agent_id": "alert-triage-bot",
  "message": { ... },
  "artifacts": [
    {
      "artifact_id": "art-001",
      "name": "Initial diagnosis",
      "description": "First-pass analysis from the agent",
      "parts": [
        { "kind": "text", "text": "CPU is 95% due to a runaway kworker process." }
      ],
      "created_at": "2026-06-15T10:00:05Z"
    }
  ],
  "created_at": "2026-06-15T10:00:00Z",
  "updated_at": "2026-06-15T10:00:05Z",
  "version": 3
}
```

**Response 404:**

```json
{
  "code": -32001,
  "message": "Task not found: task-uuid-9999"
}
```

### 7.3 REST: GET /v1/tasks (pagination)

**Query parameters:** `agent_id`, `state`, `page_size` (default 50, max 200), `page_token`.

**Response 200:**

```json
{
  "tasks": [ { ... }, { ... } ],
  "next_page_token": "eyJ0aW1lc3RhbXAiOiIyMDI2LTA2LTE1VDEwOjAwOjAwWiIsImlkIjoiLi4uIn0="
}
```

### 7.4 REST: DELETE /v1/tasks/{id}

**Response 200:**

```json
{
  "id": "task-uuid-1234",
  "state": "canceled",
  "updated_at": "2026-06-15T10:00:10Z",
  "version": 4
}
```

**Response 409 (already terminal):**

```json
{
  "code": -32002,
  "message": "Cannot transition from terminal state 'completed'",
  "data": { "task_id": "task-uuid-1234", "from": "completed" }
}
```

### 7.5 REST: POST /v1/agents

**Request:**

```json
{
  "agent_id": "alert-triage-bot",
  "name": "Alert Triage Bot",
  "description": "Investigates and triages infrastructure alerts",
  "url": "https://agents.example.com/alert-triage",
  "version": "1.4.0",
  "provider": "ExampleOps",
  "documentation": "https://docs.example.com/alert-triage",
  "skills": [
    {
      "id": "triage-cpu",
      "name": "CPU Alert Triage",
      "description": "Investigate high CPU usage",
      "tags": ["alert.triage", "monitoring", "cpu"],
      "examples": ["Why is host-42 CPU at 95%?"],
      "input_modes": ["text", "data"],
      "output_modes": ["text", "data"]
    }
  ],
  "capabilities": [
    { "name": "incident-response", "version": "1.0", "streaming": true, "push_notifications": true }
  ],
  "default_input_modes": ["text", "data"],
  "default_output_modes": ["text", "data"]
}
```

**Response 201:** echoes the registered card with `created_at` / `updated_at` populated.

### 7.6 REST: GET /v1/agents?skill=alert.triage

**Response 200:**

```json
{
  "agents": [
    { "agent_id": "alert-triage-bot", "name": "Alert Triage Bot", ... }
  ]
}
```

### 7.7 SSE: GET /v1/tasks/{id}/stream

**Response headers:** `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`.

**Event payload:**

```
event: task
id: task-uuid-1234-v3
data: {"id":"task-uuid-1234","state":"working","version":3,"updated_at":"2026-06-15T10:00:05Z",...}

event: task
id: task-uuid-1234-v4
data: {"id":"task-uuid-1234","state":"completed","version":4,"updated_at":"2026-06-15T10:00:12Z",...}

event: end
data: {}
```

Heartbeat (every 15s):

```
: heartbeat

```

### 7.8 gRPC: SubscribeTask (server streaming)

Client sends:

```protobuf
message SubscribeTaskRequest { string task_id = 1; }
```

Server streams `Task` messages on every state change. Stream closes when task reaches a terminal state.

---

## 8. Configuration

**File: `a2a/internal/config/config.go`**

All config is env-driven via `caarlos0/env/v11`.

| Env var | Type | Default | Description |
|---------|------|---------|-------------|
| `A2A_HTTP_PORT` | int | 8080 | HTTP/REST/SSE port |
| `A2A_GRPC_PORT` | int | 8443 | gRPC port |
| `A2A_METRICS_PORT` | int | 9090 | Prometheus metrics port |
| `A2A_LOG_LEVEL` | string | `info` | `debug`, `info`, `warn`, `error` |
| `A2A_SHUTDOWN_TIMEOUT` | duration | `30s` | Graceful shutdown deadline |
| `DATABASE_URL` | string | _(required)_ | PostgreSQL DSN |
| `DATABASE_MAX_CONNS` | int | 20 | pgxpool max connections |
| `DATABASE_MIN_CONNS` | int | 2 | pgxpool min connections |
| `A2A_BEARER_TOKENS` | string | _(empty)_ | Comma-separated `token:subject:scope1,scope2` |
| `A2A_MTLS_ENABLED` | bool | `false` | Enable mTLS verification |
| `A2A_MTLS_CA_FILE` | string | _(empty)_ | Path to CA bundle |
| `A2A_OAUTH_INTROSPECT_URL` | string | _(empty)_ | OAuth introspection endpoint |
| `A2A_OAUTH_CLIENT_ID` | string | _(empty)_ | OAuth client ID |
| `A2A_OAUTH_CLIENT_SECRET` | string | _(empty)_ | OAuth client secret |
| `A2A_WEBHOOK_HTTP_TIMEOUT` | duration | `10s` | Outbound webhook timeout |
| `A2A_WEBHOOK_MAX_RETRIES` | int | 5 | Max delivery attempts |
| `A2A_WEBHOOK_WORKERS` | int | 4 | Delivery worker goroutines |
| `A2A_SSE_HEARTBEAT` | duration | `15s` | SSE keepalive interval |
| `A2A_FEATURE_REDIS_PUBSUB` | bool | `false` | Enable cross-instance SSE via Redis |
| `A2A_REDIS_URL` | string | _(empty)_ | Redis URL (when feature enabled) |
| `A2A_TLS_CERT_FILE` | string | _(empty)_ | Server TLS cert |
| `A2A_TLS_KEY_FILE` | string | _(empty)_ | Server TLS key |
| `A2A_RATE_LIMIT_RPS` | int | 100 | Per-principal requests/sec |
| `A2A_RATE_LIMIT_BURST` | int | 200 | Per-principal burst |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | string | _(empty)_ | OTel collector endpoint |
| `OTEL_SERVICE_NAME` | string | `a2a-gateway` | Service name for traces |

**Feature flags:**

| Flag | Effect |
|------|--------|
| `A2A_FEATURE_REDIS_PUBSUB=true` | Use Redis pub/sub for cross-instance SSE fan-out. |
| `A2A_MTLS_ENABLED=true` | Require client certificates; reject requests without one. |
| `A2A_TLS_CERT_FILE` set | Serve HTTPS/gRPC TLS. |

---

## 9. Dependencies

**File: `a2a/go.mod` (Go 1.23.2)**

```go
module github.com/thearchitectit/a2a-gateway

go 1.23.2

require (
	github.com/caarlos0/env/v11 v11.3.1         // env-driven config
	github.com/google/uuid v1.6.0               // task IDs
	github.com/jackc/pgx/v5 v5.7.1              // PostgreSQL driver + pool
	github.com/labstack/echo/v4 v4.13.3         // HTTP framework
	github.com/prometheus/client_golang v1.20.5 // metrics
	go.opentelemetry.io/otel v1.31.0            // tracing
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.31.0
	go.opentelemetry.io/otel/sdk v1.31.0
	google.golang.org/grpc v1.67.3              // gRPC server
	google.golang.org/protobuf v1.34.2          // protobuf runtime
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.22.0 // optional REST gateway
	github.com/sony/gobreaker v1.0.0            // circuit breaker
	github.com/testcontainers/testcontainers-go v0.31.1 // test DB
	github.com/stretchr/testify v1.9.0          // test assertions
)
```

**Build tools (proto generation):**

```
protoc v25.1
protoc-gen-go v1.34.2
protoc-gen-go-grpc v1.5.1
```

---

## 10. Implementation Steps (Ordered)

Each step produces a working, testable increment.

### Step 1: Module bootstrap (Day 1)

1. Create `a2a/go.mod` with module path and Go 1.23.2.
2. Create `a2a/cmd/a2a-gateway/main.go` with a minimal `func main() { fmt.Println("a2a-gateway starting") }`.
3. Create `a2a/Makefile` with targets: `build`, `test`, `lint`, `proto`, `migrate`, `run`.
4. Verify: `cd a2a && go build ./...` succeeds.

### Step 2: Proto definitions (Day 1)

1. Write `a2a/proto/a2a.proto` (content in section 4).
2. Write `a2a/proto/buf.gen.yaml`.
3. Write `a2a/proto/Makefile`.
4. Run `make -C a2a/proto gen`.
5. Verify: `a2a/internal/pb/a2a.pb.go` and `a2a_grpc.pb.go` exist and compile.

### Step 3: Configuration package (Day 1)

1. Write `a2a/internal/config/config.go` with all env vars from section 8.
2. Write `a2a/internal/config/config_test.go` with table-driven tests for defaults.
3. Verify: `go test ./internal/config/...` passes.

### Step 4: SQL migrations (Day 2)

1. Write all six migration files from section 6.
2. Create `a2a/migrations/verify_schema.sql`.
3. Apply migrations to a local postgres: `psql $DATABASE_URL -f a2a/migrations/00{1..6}_*.up.sql`.
4. Run `psql $DATABASE_URL -f a2a/migrations/verify_schema.sql` and confirm 7 `true` rows.
5. Verify: rollback and re-apply cleanly.

### Step 5: Task types and state machine (Day 2)

1. Write `a2a/internal/task/types.go` (Task, Part, Message, Artifact).
2. Write `a2a/internal/task/state_machine.go` (transition table + validator).
3. Write `a2a/internal/task/state_machine_test.go` (all valid/invalid transitions).
4. Verify: `go test ./internal/task/...` passes.

### Step 6: Task repository (Day 3)

1. Write `a2a/internal/task/repository.go` (Repository interface + pgRepository).
2. Write `a2a/internal/task/repository_test.go` using `tests/helpers/testdb.go`.
3. Verify: `go test ./internal/task/...` passes with real DB.

### Step 7: Subscriber hub (Day 3)

1. Write `a2a/internal/task/subscribers.go`.
2. Write `a2a/internal/task/subscribers_test.go`.
3. Verify: `go test ./internal/task/...` passes.

### Step 8: Agent card types and registry (Day 4)

1. Write `a2a/internal/agentcard/types.go`.
2. Write `a2a/internal/agentcard/registry.go` (Register, Get, DiscoverBySkill, DiscoverByCapability, LoadAll, Snapshot).
3. Write `a2a/internal/agentcard/repository.go` (pgx-backed CRUD).
4. Write `a2a/internal/agentcard/registry_test.go`.
5. Verify: `go test ./internal/agentcard/...` passes.

### Step 9: Well-known discovery endpoint (Day 4)

1. Write `a2a/internal/agentcard/discover.go` (WellKnownHandler).
2. Write `a2a/internal/agentcard/discover_test.go`.
3. Verify: `GET /.well-known/agent-card.json` returns the gateway's self-card.

### Step 10: Routing engine (Day 5)

1. Write `a2a/internal/routing/types.go`.
2. Write `a2a/internal/routing/engine.go` (skill-match scoring with load penalty).
3. Write `a2a/internal/routing/engine_test.go`.
4. Verify: `go test ./internal/routing/...` passes.

### Step 11: Task manager (Day 5)

1. Write `a2a/internal/task/manager.go` (SendTask, GetTask, CancelTask, ListTasks, Subscribe).
2. Write `a2a/internal/task/manager_test.go`.
3. Verify: full lifecycle works in tests.

### Step 12: Error package (Day 6)

1. Write `a2a/internal/errors/codes.go` and `errors.go`.
2. Write `a2a/internal/errors/errors_test.go`.
3. Verify: all standard codes defined; Unwrap works.

### Step 13: Authentication (Day 6)

1. Write `a2a/internal/auth/interface.go`.
2. Write `a2a/internal/auth/bearer.go` and `bearer_test.go`.
3. Write `a2a/internal/auth/mtls.go` and `mtls_test.go`.
4. Write `a2a/internal/auth/oauth.go` and `oauth_test.go`.
5. Verify: all three authenticators pass unit tests.

### Step 14: Push notification package (Day 7)

1. Write `a2a/internal/pushnotify/types.go`.
2. Write `a2a/internal/pushnotify/signing.go`.
3. Write `a2a/internal/pushnotify/retry.go`.
4. Write `a2a/internal/pushnotify/dispatcher.go`.
5. Write `a2a/internal/pushnotify/dispatcher_test.go` with `httptest.Server` mock receiver.
6. Verify: signatures match, retries fire, max-retries fail.

### Step 15: Event-to-Task bridge (Day 7)

1. Write `a2a/internal/bridge/mapping.go` (event-type to SendTaskRequest).
2. Write `a2a/internal/bridge/handlers.go` (per-event-type converters).
3. Write `a2a/internal/bridge/event_bridge.go`.
4. Write `a2a/internal/bridge/event_bridge_test.go`.
5. Verify: each supported event type produces a valid routed task.

### Step 16: HTTP transport — middleware (Day 8)

1. Write `a2a/internal/transport/http/middleware.go` (auth, logging, rate limit, recovery).
2. Write `a2a/internal/transport/http/middleware_test.go`.
3. Verify: unauthenticated requests get 401; rate limit kicks in.

### Step 17: HTTP transport — JSON-RPC (Day 8)

1. Write `a2a/internal/transport/http/jsonrpc.go`.
2. Write `a2a/internal/transport/http/jsonrpc_test.go` covering all methods and error codes.
3. Verify: `go test ./internal/transport/http/...` passes.

### Step 18: HTTP transport — REST + SSE (Day 9)

1. Write `a2a/internal/transport/http/rest.go` (all REST endpoints).
2. Write `a2a/internal/transport/http/sse.go` (streaming handler with heartbeats).
3. Write `a2a/internal/transport/http/rest_test.go` and `sse_test.go`.
4. Verify: full curl walkthrough succeeds; SSE delivers updates and heartbeats.

### Step 19: gRPC transport (Day 9)

1. Write `a2a/internal/transport/grpc/server.go` wrapping generated service.
2. Write `a2a/internal/transport/grpc/stream.go` for `SubscribeTask`.
3. Write `a2a/internal/transport/grpc/server_test.go`.
4. Verify: grpcurl tests pass for all methods including streaming.

### Step 20: Observability (Day 10)

1. Write `a2a/internal/observability/metrics.go` (Prometheus collectors for task counts, durations, errors).
2. Write `a2a/internal/observability/tracing.go` (OTel setup).
3. Write `a2a/internal/observability/logging.go` (slog wrapper).
4. Verify: `/metrics` endpoint returns valid Prometheus output.

### Step 21: Graceful shutdown and scaling (Day 10)

1. Write `a2a/internal/scaling/shutdown.go`.
2. Write `a2a/internal/scaling/leader.go` (optional, advisory-lock-based).
3. Wire into `cmd/a2a-gateway/main.go`.
4. Verify: SIGTERM triggers clean shutdown of HTTP + gRPC.

### Step 22: Service entrypoint (Day 11)

1. Write the full `a2a/cmd/a2a-gateway/main.go`:
   - Load config.
   - Open pgx pool.
   - Run migrations (or rely on external runner).
   - Initialize registry, manager, dispatcher, subscribers, router.
   - Start HTTP server (Echo) and gRPC server.
   - Start event bridge.
   - Register signal handler.
   - Block on graceful shutdown.
2. Verify: `go run ./cmd/a2a-gateway` starts cleanly with a populated DB.

### Step 23: Integration test suite (Day 11)

1. Write `a2a/tests/helpers/testdb.go` and `testserver.go`.
2. Write all integration test files from section 5.12 inventory.
3. Verify: `go test ./tests/integration/...` passes against ephemeral testcontainers Postgres.

### Step 24: Contract tests (Day 12)

1. Write `a2a/tests/contract/jsonrpc_schema_test.go` (validate every response against JSON Schema).
2. Write `a2a/tests/contract/a2a_proto_compliance_test.go` (proto round-trip).
3. Verify: `go test ./tests/contract/...` passes.

### Step 25: Load tests (Day 12)

1. Write `a2a/tests/load/k6_throughput.js` (POST /v1/tasks at 500 RPS).
2. Write `a2a/tests/load/k6_streaming.js` (SSE 200 concurrent streams).
3. Write `a2a/tests/load/vegeta_targets.txt`.
4. Verify: run locally, p95 latency < 200ms for REST, < 100ms TTFB for SSE.

### Step 26: Deployment manifests (Day 13)

1. Write `a2a/deploy/Dockerfile` (multi-stage Go build, distroless final image).
2. Write `a2a/deploy/docker-compose.yml` (gateway + postgres + redis).
3. Write `a2a/deploy/gateway-deployment.yaml`, `gateway-service.yaml`, `gateway-ingress.yaml`, `hpa.yaml`.
4. Verify: `docker compose up` brings up the full stack.

### Step 27: Documentation (Day 13)

1. Write `a2a/docs/a2a-overview.md` (architecture + quickstart).
2. Write `a2a/docs/agent-card-spec.md` (card schema reference).
3. Write `a2a/docs/task-lifecycle.md` (state diagram + transition table).
4. Write `a2a/docs/api-reference.md` (every endpoint with examples).
5. Write `a2a/docs/deployment.md` (k8s/helm/compose instructions).

### Step 28: CI integration (Day 14)

1. Add a new stage to `ci/gitlab-ci.yml` and `ci/Jenkinsfile`:
   - `go test ./a2a/... -race -coverprofile=coverage.out`
   - `go vet ./a2a/...`
   - `golangci-lint run ./a2a/...`
   - `k6 run a2a/tests/load/k6_throughput.js` (gated, manual).
2. Verify: pipeline runs green on a feature branch.

---

## 11. Verification Checklist

Before merging, all items must be checked:

- [ ] `go build ./a2a/...` succeeds with zero warnings.
- [ ] `go test ./a2a/... -race -coverprofile=cover.out` passes with > 85% coverage.
- [ ] `golangci-lint run ./a2a/...` returns zero issues.
- [ ] `protoc --version` ≥ 25.1; generated code matches the proto.
- [ ] `psql $DATABASE_URL -f a2a/migrations/verify_schema.sql` returns all `true`.
- [ ] `curl http://localhost:8080/healthz` returns 200.
- [ ] `curl http://localhost:8080/readyz` returns 200 once DB is reachable.
- [ ] `curl http://localhost:8080/.well-known/agent-card.json` returns valid JSON.
- [ ] JSON-RPC smoke test: `tasks/send`, `tasks/get`, `tasks/cancel` all return correct shapes.
- [ ] SSE smoke test: a task transition is observed within 1s on the stream.
- [ ] gRPC smoke test: `grpcurl -plaintext localhost:8443 list` shows `a2a.v1.A2AService`.
- [ ] Webhook smoke test: a configured receiver receives a signed POST after `tasks/send`.
- [ ] Event bridge smoke test: a `ticket.opened` event on the in-process channel produces a routed task.
- [ ] Auth tests: bearer, mTLS, and OAuth each reject and accept as expected.
- [ ] Load test: 500 RPS sustained for 1 minute with p95 < 200ms and zero 5xx.
- [ ] HPA test: scale from 2 → 5 pods in response to 70% CPU.
- [ ] Graceful shutdown: SIGTERM completes in-flight requests within 30s.
- [ ] Documentation: every public endpoint is documented in `a2a/docs/api-reference.md`.

---

**End of A2A Subsystem Plan**