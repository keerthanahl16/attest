package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/keerthanahl16/attest/internal/auth"
	"github.com/keerthanahl16/attest/internal/store"
)

type ctxKey string

const (
	ctxUserID      ctxKey = "user_id"
	ctxUserEmail   ctxKey = "user_email"
	ctxWorkspaceID ctxKey = "workspace_id"
	ctxAuthMode    ctxKey = "auth_mode"
)

func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
			return
		}
		claims, err := s.Tokens.ParseAccessToken(strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid access token")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserID, claims.UserID)
		ctx = context.WithValue(ctx, ctxUserEmail, claims.Email)
		ctx = context.WithValue(ctx, ctxAuthMode, "user")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) requireAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-API-Key")
		if key == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing X-API-Key")
			return
		}
		ws, err := s.Store.ResolveAPIKey(r.Context(), auth.HashToken(key))
		if err != nil {
			if err == store.ErrNotFound {
				writeError(w, http.StatusUnauthorized, "unauthorized", "invalid API key")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal", "could not resolve API key")
			return
		}
		ctx := context.WithValue(r.Context(), ctxWorkspaceID, ws)
		ctx = context.WithValue(ctx, ctxAuthMode, "api_key")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func userIDFrom(ctx context.Context) uuid.UUID {
	v, _ := ctx.Value(ctxUserID).(uuid.UUID)
	return v
}

func workspaceIDFrom(ctx context.Context) uuid.UUID {
	v, _ := ctx.Value(ctxWorkspaceID).(uuid.UUID)
	return v
}
