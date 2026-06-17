package main

// This file is a placeholder for future route registration that needs
// to live in the cmd/server package. All HTTP route wiring is currently
// handled by internal/api.Server.registerRoutes (see internal/api/routes.go)
// which is invoked via api.NewServer -> Server.Router().
//
// Keeping this file makes it easy to add process-level middleware or
// route overrides without bloating main.go or server.go.