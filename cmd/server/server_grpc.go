package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/gateway"
	"github.com/openagentplatform/openagentplatform/a2a/spec"
	"github.com/openagentplatform/openagentplatform/internal/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// newA2AAuthenticator builds the A2A gateway Authenticator whose token
// validator reuses the API server's SessionMinter. Shared by the HTTP/A2A
// server (buildHTTPServer) and the gRPC server (buildGRPCServer).
func newA2AAuthenticator(apiServer *api.Server) *gateway.Authenticator {
	a2aAuth := gateway.NewAuthenticator(gateway.Config{RequireAuth: true})
	if sm := apiServer.SessionMinter(); sm != nil {
		a2aAuth.SetTokenValidator(func(token string) (*gateway.Identity, error) {
			claims, err := sm.Parse(token)
			if err != nil || claims == nil {
				return nil, gateway.ErrInvalidCredentials
			}
			md := map[string]string{"email": claims.Email, "role": claims.Role}
			if claims.OrgID != "" {
				md["org_id"] = claims.OrgID
			}
			scopes := []string{gateway.PermRead}
			switch claims.Role {
			case "admin", "technician", "operator":
				scopes = append(scopes, gateway.PermSend, gateway.PermAdmin)
			}
			return &gateway.Identity{
				Subject:  claims.Subject,
				Method:   gateway.AuthBearer,
				Scopes:   scopes,
				Metadata: md,
			}, nil
		})
	}
	return a2aAuth
}

// buildGRPCServer constructs the A2A gRPC server. The returned server is not
// started; the caller is responsible for Serve() and GracefulStop().
func buildGRPCServer(
	a2aGw *gateway.Gateway,
	a2aAuth *gateway.Authenticator,
	port string,
	log *slog.Logger,
) (*grpc.Server, net.Listener, error) {
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return nil, nil, err
	}

	svc := gateway.NewGRPCService(a2aGw)
	svc.SetLogger(log)

	unary := grpc.ChainUnaryInterceptor(
		authUnaryInterceptor(a2aAuth),
		loggingUnaryInterceptor(log),
		recoveryUnaryInterceptor(log),
	)
	stream := grpc.ChainStreamInterceptor(
		authStreamInterceptor(a2aAuth),
		loggingStreamInterceptor(log),
		recoveryStreamInterceptor(log),
	)

	gs := grpc.NewServer(unary, stream)
	spec.RegisterA2AServiceServer(gs, svc)
	return gs, lis, nil
}

// authUnaryInterceptor extracts a bearer token from the authorization metadata,
// authenticates it, and places the resulting Identity on the context.
func authUnaryInterceptor(auth *gateway.Authenticator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		id, err := authenticateMetadata(ctx, auth)
		if err != nil {
			return nil, err
		}
		return handler(context.WithValue(ctx, gateway.CtxKeyIdentity, id), req)
	}
}

// authStreamInterceptor is the streaming equivalent of authUnaryInterceptor.
func authStreamInterceptor(auth *gateway.Authenticator) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		id, err := authenticateMetadata(ss.Context(), auth)
		if err != nil {
			return err
		}
		return handler(srv, &identityStream{ServerStream: ss, ctx: context.WithValue(ss.Context(), gateway.CtxKeyIdentity, id)})
	}
}

func authenticateMetadata(ctx context.Context, auth *gateway.Authenticator) (*gateway.Identity, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if ids := md.Get("authorization"); len(ids) > 0 {
			// Reuse the HTTP authenticator path by building a synthetic request
			// carrying the bearer token. SetTokenValidator wires oauth2Validator,
			// which authenticateBearer consults.
			req, _ := http.NewRequest(http.MethodPost, "/a2a/grpc", nil)
			req.Header.Set("Authorization", ids[0])
			return auth.Authenticate(req)
		}
	}
	return nil, status.Error(codes.Unauthenticated, "missing authorization metadata")
}

// identityStream wraps a ServerStream, overriding Context() to return a
// context carrying the authenticated identity.
type identityStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *identityStream) Context() context.Context { return s.ctx }

// loggingUnaryInterceptor logs unary calls with latency.
func loggingUnaryInterceptor(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		if log != nil {
			log.Info("grpc call", "method", info.FullMethod, "duration_ms", time.Since(start).Milliseconds(), "error", err != nil)
		}
		return resp, err
	}
}

// loggingStreamInterceptor logs streaming calls with latency.
func loggingStreamInterceptor(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)
		if log != nil {
			log.Info("grpc stream", "method", info.FullMethod, "duration_ms", time.Since(start).Milliseconds(), "error", err != nil)
		}
		return err
	}
}

// recoveryUnaryInterceptor recovers from panics in unary handlers.
func recoveryUnaryInterceptor(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				if log != nil {
					log.Error("grpc panic recovered", "panic", r, "method", info.FullMethod)
				}
				err = status.Error(codes.Internal, "internal error")
				resp = nil
			}
		}()
		return handler(ctx, req)
	}
}

// recoveryStreamInterceptor recovers from panics in streaming handlers.
func recoveryStreamInterceptor(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if r := recover(); r != nil {
				if log != nil {
					log.Error("grpc stream panic recovered", "panic", r, "method", info.FullMethod)
				}
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(srv, ss)
	}
}
