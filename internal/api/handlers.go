package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/keerthanahl16/attest/internal/auth"
	"github.com/keerthanahl16/attest/internal/store"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.Pool().Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "unhealthy", "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type signupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Name = strings.TrimSpace(req.Name)
	if req.Email == "" || len(req.Password) < 8 || req.Name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "email, name, and password (min 8) required")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not hash password")
		return
	}
	user, err := s.Store.CreateUser(r.Context(), req.Email, req.Name, hash)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "conflict", "email already registered")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create user")
		return
	}
	s.writeSession(w, r, user)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	user, err := s.Store.GetUserByEmail(r.Context(), req.Email)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "internal", "login failed")
		return
	}
	if errors.Is(err, store.ErrNotFound) || !auth.CheckPassword(user.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid email or password")
		return
	}
	s.writeSession(w, r, user)
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "refresh_token required")
		return
	}
	newRaw, err := auth.RandomToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "token mint failed")
		return
	}
	userID, err := s.Store.RotateRefreshToken(r.Context(), auth.HashToken(req.RefreshToken), auth.HashToken(newRaw), time.Now().UTC().Add(s.Cfg.RefreshTokenTTL))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid refresh token")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "refresh failed")
		return
	}
	user, err := s.Store.GetUserByID(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "user missing")
		return
	}
	access, exp, err := s.Tokens.IssueAccessToken(user.ID, user.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "token mint failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"expires_at":    exp,
		"refresh_token": newRaw,
		"user":          map[string]any{"id": user.ID, "email": user.Email, "name": user.Name},
	})
}

func (s *Server) writeSession(w http.ResponseWriter, r *http.Request, user store.User) {
	access, exp, err := s.Tokens.IssueAccessToken(user.ID, user.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "token mint failed")
		return
	}
	refresh, err := auth.RandomToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "token mint failed")
		return
	}
	if err := s.Store.SaveRefreshToken(r.Context(), user.ID, auth.HashToken(refresh), time.Now().UTC().Add(s.Cfg.RefreshTokenTTL)); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not persist refresh token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"expires_at":    exp,
		"refresh_token": refresh,
		"user":          map[string]any{"id": user.ID, "email": user.Email, "name": user.Name},
	})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, err := s.Store.GetUserByID(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "user not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":    user.ID,
		"email": user.Email,
		"name":  user.Name,
	})
}

type createWorkspaceRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (s *Server) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	var req createWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))
	if req.Name == "" || req.Slug == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "name and slug required")
		return
	}
	ws, err := s.Store.CreateWorkspaceWithOwner(r.Context(), req.Name, req.Slug, userIDFrom(r.Context()))
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "conflict", "slug already taken")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create workspace")
		return
	}
	writeJSON(w, http.StatusCreated, ws)
}

func (s *Server) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListWorkspacesForUser(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list workspaces")
		return
	}
	if list == nil {
		list = []store.Workspace{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspaces": list})
}

func (s *Server) withWorkspace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "workspaceID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid workspace id")
			return
		}
		ok, err := s.Store.UserInWorkspace(r.Context(), id, userIDFrom(r.Context()))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "membership check failed")
			return
		}
		if !ok {
			writeError(w, http.StatusForbidden, "forbidden", "not a workspace member")
			return
		}
		ctx := context.WithValue(r.Context(), ctxWorkspaceID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type createToolRequest struct {
	Name             string `json:"name"`
	Description      string `json:"description"`
	RiskLevel        string `json:"risk_level"`
	RequiresApproval bool   `json:"requires_approval"`
	MockHandler      string `json:"mock_handler"`
}

func (s *Server) handleCreateTool(w http.ResponseWriter, r *http.Request) {
	var req createToolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "name required")
		return
	}
	if req.RiskLevel == "" {
		req.RiskLevel = "safe"
	}
	if req.MockHandler == "" {
		req.MockHandler = "echo"
	}
	if req.RiskLevel != "safe" && req.RiskLevel != "irreversible" {
		writeError(w, http.StatusBadRequest, "bad_request", "risk_level must be safe or irreversible")
		return
	}
	if req.RiskLevel == "irreversible" {
		req.RequiresApproval = true
	}
	t, err := s.Store.CreateTool(r.Context(), store.Tool{
		WorkspaceID:      workspaceIDFrom(r.Context()),
		Name:             req.Name,
		Description:      req.Description,
		RiskLevel:        req.RiskLevel,
		RequiresApproval: req.RequiresApproval,
		MockHandler:      req.MockHandler,
	})
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "conflict", "tool name already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create tool")
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) handleListTools(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListTools(r.Context(), workspaceIDFrom(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list tools")
		return
	}
	if list == nil {
		list = []store.Tool{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": list})
}

type createAPIKeyRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req createAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = "agent"
	}
	raw, err := auth.RandomToken(24)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "key mint failed")
		return
	}
	apiKey := "atk_" + raw
	prefix := apiKey
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	meta, err := s.Store.CreateAPIKey(r.Context(), workspaceIDFrom(r.Context()), req.Name, prefix, auth.HashToken(apiKey))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create API key")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"api_key": apiKey,
		"meta":    meta,
		"note":    "Copy the api_key now. Attest stores only a hash.",
	})
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListRuns(r.Context(), workspaceIDFrom(r.Context()), 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list runs")
		return
	}
	if list == nil {
		list = []store.Run{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": list})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	runID, err := uuid.Parse(chi.URLParam(r, "runID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid run id")
		return
	}
	run, err := s.Store.GetRun(r.Context(), workspaceIDFrom(r.Context()), runID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "run not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load run")
		return
	}
	events, err := s.Store.ListEventsForRun(r.Context(), workspaceIDFrom(r.Context()), runID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load events")
		return
	}
	if events == nil {
		events = []store.Event{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": run, "events": events})
}

func (s *Server) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListPendingApprovals(r.Context(), workspaceIDFrom(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list approvals")
		return
	}
	if list == nil {
		list = []store.Approval{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"approvals": list})
}

type decideRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	s.decide(w, r, true)
}

func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	s.decide(w, r, false)
}

func (s *Server) decide(w http.ResponseWriter, r *http.Request, approve bool) {
	id, err := uuid.Parse(chi.URLParam(r, "approvalID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid approval id")
		return
	}
	var req decideRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	a, err := s.Store.DecideApproval(r.Context(), workspaceIDFrom(r.Context()), id, userIDFrom(r.Context()), approve, req.Reason)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "approval not found")
		return
	}
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "conflict", "approval already decided")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "decision failed")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListRecentEvents(r.Context(), workspaceIDFrom(r.Context()), 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list events")
		return
	}
	if list == nil {
		list = []store.Event{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": list})
}

type createRunRequest struct {
	IdempotencyKey string          `json:"idempotency_key"`
	Tool           string          `json:"tool"`
	Input          json.RawMessage `json:"input"`
}

func (s *Server) handleCreateRun(w http.ResponseWriter, r *http.Request) {
	var req createRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.Tool = strings.TrimSpace(req.Tool)
	if req.IdempotencyKey == "" || req.Tool == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "idempotency_key and tool required")
		return
	}
	if req.Input == nil {
		req.Input = json.RawMessage(`{}`)
	}
	result, err := s.Store.CreateRun(r.Context(), store.CreateRunParams{
		WorkspaceID:    workspaceIDFrom(r.Context()),
		IdempotencyKey: req.IdempotencyKey,
		InputJSON:      req.Input,
		ToolName:       req.Tool,
		StepInput:      req.Input,
	})
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "tool not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create run")
		return
	}
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{
		"run":      result.Run,
		"replayed": result.Replayed,
	})
}

func (s *Server) handleAgentGetRun(w http.ResponseWriter, r *http.Request) {
	runID, err := uuid.Parse(chi.URLParam(r, "runID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid run id")
		return
	}
	run, err := s.Store.GetRun(r.Context(), workspaceIDFrom(r.Context()), runID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "run not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load run")
		return
	}
	writeJSON(w, http.StatusOK, run)
}
