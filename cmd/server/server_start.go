package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/resilience"
	"github.com/openagentplatform/openagentplatform/internal/telemetry"
)

func (s *Server) Start(ctx context.Context) error {
	// Start event subscriptions after the HTTP server has had a chance to
	// bind so /api/v1/agents/register accepts first contact from agents
	// before any heartbeat traffic starts.
	hbCtx, hbCancel := context.WithCancel(context.Background())
	defer hbCancel()

	if err := s.heartbeat.Start(hbCtx); err != nil {
		return errors.New("heartbeat handler start: " + err.Error())
	}
	if err := s.dispatcher.Start(hbCtx); err != nil {
		return errors.New("check dispatcher start: " + err.Error())
	}
	if err := s.ingestor.Start(hbCtx); err != nil {
		return errors.New("result ingestor start: " + err.Error())
	}
	if err := s.alertEngine.Start(hbCtx); err != nil {
		return errors.New("alert engine start: " + err.Error())
	}
	if err := s.policyEngine.Start(hbCtx); err != nil {
		return errors.New("policy engine start: " + err.Error())
	}
	if err := s.eventBridge.Start(); err != nil {
		return errors.New("a2a event bridge start: " + err.Error())
	}
	if err := s.rpcBridge.Start(); err != nil {
		return errors.New("a2a rpc bridge start: " + err.Error())
	}

	// Start the secrets TTL sweeper so expired credential injections
	// (env vars, temp files, stdin pipes) are cleaned up automatically.
	if s.secretsSweeper != nil {
		s.secretsSweeper.Start(hbCtx)
	}

	// Start the per-tenant retention purger (daily soft/hard delete).
	if s.retentionPurger != nil {
		s.retentionPurger.Start(hbCtx)
	}

	go s.patchScheduler.Run(hbCtx)

	// Start the billing sync loop and the metering flush loop, sharing hbCtx
	// so they are cancelled on shutdown. The metering queue is flushed one
	// final time in Shutdown() before the process exits.
	if s.meteringSvc != nil {
		s.meteringSvc.StartFlushLoop(hbCtx)
	}
	if s.billingSvc != nil {
		s.billingSvc.StartSyncLoop(hbCtx)
	}

	go func() {
		s.log.Info("starting server", "addr", s.httpServer.Addr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.Error("server error", "err", err)
		}
	}()

	// Start the A2A gRPC transport on its own port. The gRPC server is optional
	// (nil when the port could not be bound in NewServer); skip silently.
	if s.grpcServer != nil && s.grpcListener != nil {
		go func() {
			s.log.Info("starting grpc server", "addr", s.grpcListener.Addr().String())
			if err := s.grpcServer.Serve(s.grpcListener); err != nil {
				s.log.Error("grpc server error", "err", err)
			}
		}()
	}

	return nil
}

// Shutdown gracefully stops all background handlers and the HTTP server.
// It blocks until the shutdown completes or the context is cancelled.
//
// The shutdown sequence is:
//  1. Stop accepting new HTTP requests and wait for in-flight ones to
//     drain (delegated to the resilience.GracefulShutdown coordinator).
//  2. Stop background handlers in reverse-initialisation order.
//  3. Close the NATS client (drains subscriptions).
//  4. Close the database pool.
//  5. Shut down the OpenTelemetry tracer provider.
func (s *Server) Shutdown(ctx context.Context) error {
	// Register all closers with the graceful shutdown coordinator.
	// They are closed in LIFO order after the HTTP server has drained.
	//
	// 1. Background workers and engines.
	s.graceful.Register("heartbeat", resilience.CloserFunc(func(_ context.Context) error {
		s.heartbeat.Stop()
		return nil
	}))
	s.graceful.Register("dispatcher", resilience.CloserFunc(func(_ context.Context) error {
		s.dispatcher.Stop()
		return nil
	}))
	s.graceful.Register("ingestor", resilience.CloserFunc(func(_ context.Context) error {
		s.ingestor.Stop()
		return nil
	}))
	s.graceful.Register("alert-engine", resilience.CloserFunc(func(_ context.Context) error {
		s.alertEngine.Stop()
		return nil
	}))
	s.graceful.Register("policy-engine", resilience.CloserFunc(func(_ context.Context) error {
		s.policyEngine.Stop()
		return nil
	}))
	s.graceful.Register("event-bridge", resilience.CloserFunc(func(_ context.Context) error {
		s.eventBridge.Stop()
		return nil
	}))
	s.graceful.Register("rpc-bridge", resilience.CloserFunc(func(_ context.Context) error {
		s.rpcBridge.Stop()
		return nil
	}))
	// gRPC server: stop accepting new RPCs and wait for in-flight calls to
	// finish before closing the listener.
	if s.grpcServer != nil {
		s.graceful.Register("grpc-server", resilience.CloserFunc(func(_ context.Context) error {
			s.grpcServer.GracefulStop()
			return nil
		}))
	}
	s.graceful.Register("patch-scheduler", resilience.CloserFunc(func(_ context.Context) error {
		s.patchScheduler.Close()
		return nil
	}))

	// Flush any queued metering usage to Stripe so it isn't lost on shutdown.
	// Use a bounded context independent of the request/shutdown ctx so the
	// report isn't cancelled prematurely.
	if s.meteringSvc != nil {
		s.graceful.Register("metering-flush", resilience.CloserFunc(func(_ context.Context) error {
			flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := s.meteringSvc.Flush(flushCtx); err != nil {
				s.log.Warn("billing: final metering flush returned errors", "err", err)
			}
			return nil
		}))
	}

	// 2. Secrets sweeper.
	if s.secretsSweeper != nil {
		s.graceful.Register("secrets-sweeper", resilience.CloserFunc(func(_ context.Context) error {
			s.secretsSweeper.Stop()
			return nil
		}))
	}

	// 2b. Tenancy retention purger.
	if s.retentionPurger != nil {
		s.graceful.Register("retention-purger", resilience.CloserFunc(func(_ context.Context) error {
			s.retentionPurger.Stop()
			return nil
		}))
	}

	// 3. Rate limiter janitor.
	s.graceful.Register("rate-limiter", resilience.CloserFunc(func(_ context.Context) error {
		s.rateLimiter.Stop()
		return nil
	}))

	// 4. NATS client (drains subscriptions internally).
	if s.natsClient != nil {
		s.graceful.Register("nats-client", resilience.CloserFunc(func(_ context.Context) error {
			s.natsClient.Close()
			return nil
		}))
	}

	// 5. Database pool.
	if s.pool != nil {
		s.graceful.Register("db-pool", resilience.CloserFunc(func(_ context.Context) error {
			s.pool.Close()
			return nil
		}))
	}

	// 6. Tracer provider (flushes spans).
	s.graceful.Register("tracer-provider", resilience.CloserFunc(func(_ context.Context) error {
		_ = telemetry.Shutdown(ctx, s.tracerProvider)
		return nil
	}))

	// Execute the full shutdown sequence: HTTP drain, then dependency teardown.
	return s.graceful.ShutdownAll(s.httpServer)
}

// newAuditService is a thin constructor for the audit service. Kept here
// so NewServer stays self-contained.
