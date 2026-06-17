package main

import (
	"net/http"

	"github.com/openagentplatform/openagentplatform/a2a/gateway"
	"github.com/openagentplatform/openagentplatform/internal/telemetry"
)

// withTracing wraps the top-level HTTP handler with the OpenTelemetry HTTP
// middleware.  Every request receives a server span, with health-check
// endpoints skipped.  Trace context is extracted from incoming request
// headers and X-Request-ID is propagated via baggage.
func withTracing(next http.Handler) http.Handler {
	return telemetry.HTTPMiddleware()(next)
}

// newA2ARouter builds a top-level HTTP handler that delegates all requests
// to the API server and mounts the A2A gateway handlers under /a2a/.
//
// Layout:
//
//	/a2a/                - JSON-RPC 2.0 endpoint (POST)
//	/a2a/v1/tasks        - REST task CRUD
//	/a2a/v1/tasks/{id}   - REST single task
//	/a2a/v1/tasks/{id}/subscribe - SSE task status stream
//	/a2a/v1/agents       - REST agent listing
//	/a2a/v1/agents/{name} - REST single agent card
func newA2ARouter(apiHandler http.Handler, gw *gateway.Gateway) http.Handler {
	root := http.NewServeMux()

	// JSON-RPC endpoint at /a2a/
	root.Handle("/a2a/", a2aJSONRPC(gw))

	// REST task endpoints
	root.Handle("/a2a/v1/tasks", gateway.RESTTasksHandler(gw))
	root.Handle("/a2a/v1/tasks/", a2aTaskSubroutes(gw))

	// REST agent endpoints
	root.Handle("/a2a/v1/agents", gateway.RESTAgentsHandler(gw))
	root.Handle("/a2a/v1/agents/", gateway.RESTAgentHandler(gw))

	// Composite handler: API server for everything else, A2A for /a2a/*
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/a2a" {
			root.ServeHTTP(w, r)
			return
		}
		apiHandler.ServeHTTP(w, r)
	})
}

// a2aJSONRPC handles POST /a2a/ by delegating to the JSON-RPC handler.
func a2aJSONRPC(gw *gateway.Gateway) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			gateway.JSONRPCHandler(gw).ServeHTTP(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})
}

// a2aTaskSubroutes dispatches /a2a/v1/tasks/{id} and
// /a2a/v1/tasks/{id}/subscribe to the correct REST handler.
func a2aTaskSubroutes(gw *gateway.Gateway) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// Strip the prefix to inspect the remainder: {id} or {id}/subscribe
		remainder := path[len("/a2a/v1/tasks/"):]
		if remainder == "" {
			http.NotFound(w, r)
			return
		}

		// Check for /subscribe suffix
		const subscribeSuffix = "/subscribe"
		if len(remainder) > len(subscribeSuffix) &&
			remainder[len(remainder)-len(subscribeSuffix):] == subscribeSuffix {
			taskID := remainder[:len(remainder)-len(subscribeSuffix)]
			if taskID == "" {
				http.NotFound(w, r)
				return
			}
			gateway.RESTTaskSubscribeHandler(gw).ServeHTTP(w, r)
			return
		}

		// Single task handler: GET, DELETE /a2a/v1/tasks/{id}
		gateway.RESTTaskHandler(gw).ServeHTTP(w, r)
	})
}
