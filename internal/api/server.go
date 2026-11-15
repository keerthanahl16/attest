// Package api exposes the Attest HTTP surface for humans (JWT) and agents (API keys).
package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/keerthanahl16/attest/internal/auth"
	"github.com/keerthanahl16/attest/internal/config"
	"github.com/keerthanahl16/attest/internal/store"
)

// Server wires HTTP handlers to persistence and auth helpers.
type Server struct {
	Cfg    config.Config
	Store  *store.Store
	Tokens *auth.TokenService
	Log    *slog.Logger
}

// Router builds the chi router with public, human, and agent routes.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Timeout(60 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   s.Cfg.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-API-Key"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/healthz", s.handleHealth)

	r.Route("/v1", func(r chi.Router) {
		r.Post("/auth/signup", s.handleSignup)
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/refresh", s.handleRefresh)

		r.Group(func(r chi.Router) {
			r.Use(s.requireUser)
			r.Get("/me", s.handleMe)
			r.Get("/workspaces", s.handleListWorkspaces)
			r.Post("/workspaces", s.handleCreateWorkspace)

			r.Route("/workspaces/{workspaceID}", func(r chi.Router) {
				r.Use(s.withWorkspace)
				r.Get("/tools", s.handleListTools)
				r.Post("/tools", s.handleCreateTool)
				r.Post("/api-keys", s.handleCreateAPIKey)
				r.Get("/runs", s.handleListRuns)
				r.Get("/runs/{runID}", s.handleGetRun)
				r.Get("/approvals", s.handleListApprovals)
				r.Post("/approvals/{approvalID}/approve", s.handleApprove)
				r.Post("/approvals/{approvalID}/reject", s.handleReject)
				r.Get("/events", s.handleListEvents)
			})
		})

		// Agent surface — authenticated with X-API-Key.
		r.Group(func(r chi.Router) {
			r.Use(s.requireAPIKey)
			r.Post("/agent/runs", s.handleCreateRun)
			r.Get("/agent/runs/{runID}", s.handleAgentGetRun)
		})
	})

	return r
}
