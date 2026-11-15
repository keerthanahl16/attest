// Package store is the PostgreSQL persistence layer for Attest.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned on unique-constraint collisions (e.g. duplicate email).
var ErrConflict = errors.New("conflict")

// Store wraps a pgx pool.
type Store struct {
	pool *pgxpool.Pool
}

// New constructs a Store.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Pool exposes the underlying pool for health checks.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// CreateUser inserts a new user.
func (s *Store) CreateUser(ctx context.Context, email, name, passwordHash string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (email, name, password_hash)
		VALUES ($1, $2, $3)
		RETURNING id, email, name, password_hash, created_at`,
		strings.ToLower(email), name, passwordHash,
	).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.CreatedAt)
	if isUniqueViolation(err) {
		return User{}, ErrConflict
	}
	return u, err
}

// GetUserByEmail loads a user by email.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, name, password_hash, created_at
		FROM users WHERE email=$1`, strings.ToLower(email),
	).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

// GetUserByID loads a user by id.
func (s *Store) GetUserByID(ctx context.Context, id uuid.UUID) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, name, password_hash, created_at
		FROM users WHERE id=$1`, id,
	).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

// SaveRefreshToken stores a hashed refresh token.
func (s *Store) SaveRefreshToken(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)`, userID, tokenHash, expiresAt)
	return err
}

// RotateRefreshToken revokes the old hash and inserts a new one atomically.
func (s *Store) RotateRefreshToken(ctx context.Context, oldHash, newHash string, expiresAt time.Time) (uuid.UUID, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)

	var userID uuid.UUID
	err = tx.QueryRow(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = now()
		WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at > now()
		RETURNING user_id`, oldHash,
	).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)`, userID, newHash, expiresAt); err != nil {
		return uuid.Nil, err
	}
	return userID, tx.Commit(ctx)
}

// CreateWorkspaceWithOwner creates a workspace and owner membership.
func (s *Store) CreateWorkspaceWithOwner(ctx context.Context, name, slug string, ownerID uuid.UUID) (Workspace, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Workspace{}, err
	}
	defer tx.Rollback(ctx)

	var w Workspace
	err = tx.QueryRow(ctx, `
		INSERT INTO workspaces (name, slug) VALUES ($1, $2)
		RETURNING id, name, slug, created_at`, name, slug,
	).Scan(&w.ID, &w.Name, &w.Slug, &w.CreatedAt)
	if isUniqueViolation(err) {
		return Workspace{}, ErrConflict
	}
	if err != nil {
		return Workspace{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')`, w.ID, ownerID); err != nil {
		return Workspace{}, err
	}
	return w, tx.Commit(ctx)
}

// ListWorkspacesForUser returns workspaces the user belongs to.
func (s *Store) ListWorkspacesForUser(ctx context.Context, userID uuid.UUID) ([]Workspace, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT w.id, w.name, w.slug, w.created_at
		FROM workspaces w
		JOIN workspace_members m ON m.workspace_id = w.id
		WHERE m.user_id=$1
		ORDER BY w.created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Workspace
	for rows.Next() {
		var w Workspace
		if err := rows.Scan(&w.ID, &w.Name, &w.Slug, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// UserInWorkspace reports whether the user is a member.
func (s *Store) UserInWorkspace(ctx context.Context, workspaceID, userID uuid.UUID) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM workspace_members WHERE workspace_id=$1 AND user_id=$2
		)`, workspaceID, userID).Scan(&ok)
	return ok, err
}

// CreateAPIKey stores a hashed API key and returns metadata.
func (s *Store) CreateAPIKey(ctx context.Context, workspaceID uuid.UUID, name, prefix, hash string) (APIKeyMeta, error) {
	var k APIKeyMeta
	err := s.pool.QueryRow(ctx, `
		INSERT INTO api_keys (workspace_id, name, key_prefix, key_hash)
		VALUES ($1, $2, $3, $4)
		RETURNING id, workspace_id, name, key_prefix, created_at, revoked_at`,
		workspaceID, name, prefix, hash,
	).Scan(&k.ID, &k.WorkspaceID, &k.Name, &k.KeyPrefix, &k.CreatedAt, &k.RevokedAt)
	return k, err
}

// ResolveAPIKey returns the workspace for a raw key hash.
func (s *Store) ResolveAPIKey(ctx context.Context, hash string) (uuid.UUID, error) {
	var ws uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT workspace_id FROM api_keys
		WHERE key_hash=$1 AND revoked_at IS NULL`, hash,
	).Scan(&ws)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	return ws, err
}

// CreateTool registers a tool in a workspace.
func (s *Store) CreateTool(ctx context.Context, t Tool) (Tool, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO tools (workspace_id, name, description, risk_level, requires_approval, mock_handler)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, workspace_id, name, description, risk_level, requires_approval, mock_handler, created_at`,
		t.WorkspaceID, t.Name, t.Description, t.RiskLevel, t.RequiresApproval, t.MockHandler,
	).Scan(&t.ID, &t.WorkspaceID, &t.Name, &t.Description, &t.RiskLevel, &t.RequiresApproval, &t.MockHandler, &t.CreatedAt)
	if isUniqueViolation(err) {
		return Tool{}, ErrConflict
	}
	return t, err
}

// ListTools returns tools for a workspace.
func (s *Store) ListTools(ctx context.Context, workspaceID uuid.UUID) ([]Tool, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_id, name, description, risk_level, requires_approval, mock_handler, created_at
		FROM tools WHERE workspace_id=$1 ORDER BY name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tool
	for rows.Next() {
		var t Tool
		if err := rows.Scan(&t.ID, &t.WorkspaceID, &t.Name, &t.Description, &t.RiskLevel, &t.RequiresApproval, &t.MockHandler, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetToolByName loads a tool by workspace + name.
func (s *Store) GetToolByName(ctx context.Context, workspaceID uuid.UUID, name string) (Tool, error) {
	var t Tool
	err := s.pool.QueryRow(ctx, `
		SELECT id, workspace_id, name, description, risk_level, requires_approval, mock_handler, created_at
		FROM tools WHERE workspace_id=$1 AND name=$2`, workspaceID, name,
	).Scan(&t.ID, &t.WorkspaceID, &t.Name, &t.Description, &t.RiskLevel, &t.RequiresApproval, &t.MockHandler, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tool{}, ErrNotFound
	}
	return t, err
}

// GetToolByID loads a tool by id.
func (s *Store) GetToolByID(ctx context.Context, id uuid.UUID) (Tool, error) {
	var t Tool
	err := s.pool.QueryRow(ctx, `
		SELECT id, workspace_id, name, description, risk_level, requires_approval, mock_handler, created_at
		FROM tools WHERE id=$1`, id,
	).Scan(&t.ID, &t.WorkspaceID, &t.Name, &t.Description, &t.RiskLevel, &t.RequiresApproval, &t.MockHandler, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tool{}, ErrNotFound
	}
	return t, err
}

// CreateRunParams is input for creating an idempotent run.
type CreateRunParams struct {
	WorkspaceID    uuid.UUID
	IdempotencyKey string
	InputJSON      json.RawMessage
	ToolName       string
	StepInput      json.RawMessage
}

// CreateRunResult is returned after create-or-replay.
type CreateRunResult struct {
	Run      Run
	Replayed bool
}

// CreateRun inserts a run + step + job (or returns the existing run for the same idempotency key).
func (s *Store) CreateRun(ctx context.Context, p CreateRunParams) (CreateRunResult, error) {
	tool, err := s.GetToolByName(ctx, p.WorkspaceID, p.ToolName)
	if err != nil {
		return CreateRunResult{}, err
	}
	if p.StepInput == nil {
		p.StepInput = json.RawMessage(`{}`)
	}
	if p.InputJSON == nil {
		p.InputJSON = json.RawMessage(`{}`)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return CreateRunResult{}, err
	}
	defer tx.Rollback(ctx)

	var existing Run
	err = tx.QueryRow(ctx, `
		SELECT id, workspace_id, idempotency_key, status, input_json, result_json, error_message, created_at, updated_at
		FROM runs WHERE workspace_id=$1 AND idempotency_key=$2`,
		p.WorkspaceID, p.IdempotencyKey,
	).Scan(&existing.ID, &existing.WorkspaceID, &existing.IdempotencyKey, &existing.Status, &existing.InputJSON, &existing.ResultJSON, &existing.ErrorMessage, &existing.CreatedAt, &existing.UpdatedAt)
	if err == nil {
		steps, err := s.listStepsTx(ctx, tx, existing.ID)
		if err != nil {
			return CreateRunResult{}, err
		}
		existing.Steps = steps
		return CreateRunResult{Run: existing, Replayed: true}, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return CreateRunResult{}, err
	}

	stepStatus := "pending"
	runStatus := "queued"
	if tool.RequiresApproval {
		stepStatus = "awaiting_approval"
		runStatus = "awaiting_approval"
	}

	var run Run
	err = tx.QueryRow(ctx, `
		INSERT INTO runs (workspace_id, idempotency_key, status, input_json)
		VALUES ($1, $2, $3, $4)
		RETURNING id, workspace_id, idempotency_key, status, input_json, result_json, error_message, created_at, updated_at`,
		p.WorkspaceID, p.IdempotencyKey, runStatus, p.InputJSON,
	).Scan(&run.ID, &run.WorkspaceID, &run.IdempotencyKey, &run.Status, &run.InputJSON, &run.ResultJSON, &run.ErrorMessage, &run.CreatedAt, &run.UpdatedAt)
	if err != nil {
		return CreateRunResult{}, err
	}

	var step RunStep
	err = tx.QueryRow(ctx, `
		INSERT INTO run_steps (run_id, workspace_id, tool_id, step_index, status, input_json)
		VALUES ($1, $2, $3, 0, $4, $5)
		RETURNING id, run_id, workspace_id, tool_id, step_index, status, input_json, output_json, error_message, attempt_count, created_at, updated_at`,
		run.ID, p.WorkspaceID, tool.ID, stepStatus, p.StepInput,
	).Scan(&step.ID, &step.RunID, &step.WorkspaceID, &step.ToolID, &step.StepIndex, &step.Status, &step.InputJSON, &step.OutputJSON, &step.ErrorMessage, &step.AttemptCount, &step.CreatedAt, &step.UpdatedAt)
	if err != nil {
		return CreateRunResult{}, err
	}
	step.ToolName = tool.Name

	if tool.RequiresApproval {
		if _, err := tx.Exec(ctx, `
			INSERT INTO approvals (step_id, run_id, workspace_id, status)
			VALUES ($1, $2, $3, 'pending')`, step.ID, run.ID, p.WorkspaceID); err != nil {
			return CreateRunResult{}, err
		}
		if err := appendEventTx(ctx, tx, p.WorkspaceID, &run.ID, &step.ID, "approval_requested",
			fmt.Sprintf("Tool %q requires human approval before execution", tool.Name), nil); err != nil {
			return CreateRunResult{}, err
		}
	} else {
		if _, err := tx.Exec(ctx, `
			INSERT INTO jobs (workspace_id, step_id, job_type, status, available_at)
			VALUES ($1, $2, 'execute_step', 'pending', now())`, p.WorkspaceID, step.ID); err != nil {
			return CreateRunResult{}, err
		}
		if err := appendEventTx(ctx, tx, p.WorkspaceID, &run.ID, &step.ID, "run_queued",
			fmt.Sprintf("Run queued for safe tool %q", tool.Name), nil); err != nil {
			return CreateRunResult{}, err
		}
	}

	run.Steps = []RunStep{step}
	if err := tx.Commit(ctx); err != nil {
		return CreateRunResult{}, err
	}
	return CreateRunResult{Run: run, Replayed: false}, nil
}

func (s *Store) listStepsTx(ctx context.Context, tx pgx.Tx, runID uuid.UUID) ([]RunStep, error) {
	rows, err := tx.Query(ctx, `
		SELECT rs.id, rs.run_id, rs.workspace_id, rs.tool_id, t.name, rs.step_index, rs.status,
		       rs.input_json, rs.output_json, rs.error_message, rs.attempt_count, rs.created_at, rs.updated_at
		FROM run_steps rs
		JOIN tools t ON t.id = rs.tool_id
		WHERE rs.run_id=$1
		ORDER BY rs.step_index`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunStep
	for rows.Next() {
		var st RunStep
		if err := rows.Scan(&st.ID, &st.RunID, &st.WorkspaceID, &st.ToolID, &st.ToolName, &st.StepIndex, &st.Status,
			&st.InputJSON, &st.OutputJSON, &st.ErrorMessage, &st.AttemptCount, &st.CreatedAt, &st.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// GetRun returns a run with steps.
func (s *Store) GetRun(ctx context.Context, workspaceID, runID uuid.UUID) (Run, error) {
	var run Run
	err := s.pool.QueryRow(ctx, `
		SELECT id, workspace_id, idempotency_key, status, input_json, result_json, error_message, created_at, updated_at
		FROM runs WHERE id=$1 AND workspace_id=$2`, runID, workspaceID,
	).Scan(&run.ID, &run.WorkspaceID, &run.IdempotencyKey, &run.Status, &run.InputJSON, &run.ResultJSON, &run.ErrorMessage, &run.CreatedAt, &run.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT rs.id, rs.run_id, rs.workspace_id, rs.tool_id, t.name, rs.step_index, rs.status,
		       rs.input_json, rs.output_json, rs.error_message, rs.attempt_count, rs.created_at, rs.updated_at
		FROM run_steps rs
		JOIN tools t ON t.id = rs.tool_id
		WHERE rs.run_id=$1
		ORDER BY rs.step_index`, runID)
	if err != nil {
		return Run{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var st RunStep
		if err := rows.Scan(&st.ID, &st.RunID, &st.WorkspaceID, &st.ToolID, &st.ToolName, &st.StepIndex, &st.Status,
			&st.InputJSON, &st.OutputJSON, &st.ErrorMessage, &st.AttemptCount, &st.CreatedAt, &st.UpdatedAt); err != nil {
			return Run{}, err
		}
		run.Steps = append(run.Steps, st)
	}
	return run, rows.Err()
}

// ListRuns returns recent runs for a workspace.
func (s *Store) ListRuns(ctx context.Context, workspaceID uuid.UUID, limit int) ([]Run, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_id, idempotency_key, status, input_json, result_json, error_message, created_at, updated_at
		FROM runs WHERE workspace_id=$1
		ORDER BY created_at DESC LIMIT $2`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.WorkspaceID, &r.IdempotencyKey, &r.Status, &r.InputJSON, &r.ResultJSON, &r.ErrorMessage, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListPendingApprovals returns open approval requests.
func (s *Store) ListPendingApprovals(ctx context.Context, workspaceID uuid.UUID) ([]Approval, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.step_id, a.run_id, a.workspace_id, a.status, a.requested_at, a.decided_at, a.decided_by, a.reason,
		       t.name, rs.input_json, r.idempotency_key
		FROM approvals a
		JOIN run_steps rs ON rs.id = a.step_id
		JOIN tools t ON t.id = rs.tool_id
		JOIN runs r ON r.id = a.run_id
		WHERE a.workspace_id=$1 AND a.status='pending'
		ORDER BY a.requested_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Approval
	for rows.Next() {
		var a Approval
		if err := rows.Scan(&a.ID, &a.StepID, &a.RunID, &a.WorkspaceID, &a.Status, &a.RequestedAt, &a.DecidedAt, &a.DecidedBy, &a.Reason,
			&a.ToolName, &a.StepInput, &a.RunIdemKey); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DecideApproval approves or rejects a pending step and enqueues work when approved.
func (s *Store) DecideApproval(ctx context.Context, workspaceID, approvalID, userID uuid.UUID, approve bool, reason string) (Approval, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Approval{}, err
	}
	defer tx.Rollback(ctx)

	var a Approval
	err = tx.QueryRow(ctx, `
		SELECT id, step_id, run_id, workspace_id, status, requested_at, decided_at, decided_by, reason
		FROM approvals
		WHERE id=$1 AND workspace_id=$2
		FOR UPDATE`, approvalID, workspaceID,
	).Scan(&a.ID, &a.StepID, &a.RunID, &a.WorkspaceID, &a.Status, &a.RequestedAt, &a.DecidedAt, &a.DecidedBy, &a.Reason)
	if errors.Is(err, pgx.ErrNoRows) {
		return Approval{}, ErrNotFound
	}
	if err != nil {
		return Approval{}, err
	}
	if a.Status != "pending" {
		return Approval{}, fmt.Errorf("%w: approval already decided", ErrConflict)
	}

	newStatus := "rejected"
	stepStatus := "rejected"
	runStatus := "rejected"
	kind := "approval_rejected"
	msg := "Human rejected the tool call"
	if approve {
		newStatus = "approved"
		stepStatus = "pending"
		runStatus = "queued"
		kind = "approval_granted"
		msg = "Human approved the tool call"
	}

	now := time.Now().UTC()
	err = tx.QueryRow(ctx, `
		UPDATE approvals
		SET status=$1, decided_at=$2, decided_by=$3, reason=$4
		WHERE id=$5
		RETURNING id, step_id, run_id, workspace_id, status, requested_at, decided_at, decided_by, reason`,
		newStatus, now, userID, nullIfEmpty(reason), approvalID,
	).Scan(&a.ID, &a.StepID, &a.RunID, &a.WorkspaceID, &a.Status, &a.RequestedAt, &a.DecidedAt, &a.DecidedBy, &a.Reason)
	if err != nil {
		return Approval{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE run_steps SET status=$1, updated_at=now() WHERE id=$2`, stepStatus, a.StepID); err != nil {
		return Approval{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE runs SET status=$1, updated_at=now() WHERE id=$2`, runStatus, a.RunID); err != nil {
		return Approval{}, err
	}

	if approve {
		if _, err := tx.Exec(ctx, `
			INSERT INTO jobs (workspace_id, step_id, job_type, status, available_at)
			VALUES ($1, $2, 'execute_step', 'pending', now())`, workspaceID, a.StepID); err != nil {
			return Approval{}, err
		}
	}

	if err := appendEventTx(ctx, tx, workspaceID, &a.RunID, &a.StepID, kind, msg, map[string]any{
		"decided_by": userID.String(),
		"reason":     reason,
	}); err != nil {
		return Approval{}, err
	}

	return a, tx.Commit(ctx)
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func appendEventTx(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID, runID, stepID *uuid.UUID, kind, message string, meta map[string]any) error {
	metaJSON := json.RawMessage(`{}`)
	if meta != nil {
		b, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		metaJSON = b
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO events (workspace_id, run_id, step_id, kind, message, meta_json)
		VALUES ($1, $2, $3, $4, $5, $6)`, workspaceID, runID, stepID, kind, message, metaJSON)
	return err
}

// AppendEvent writes an audit event outside a larger transaction.
func (s *Store) AppendEvent(ctx context.Context, workspaceID uuid.UUID, runID, stepID *uuid.UUID, kind, message string, meta map[string]any) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := appendEventTx(ctx, tx, workspaceID, runID, stepID, kind, message, meta); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ListEventsForRun returns the timeline for a run.
func (s *Store) ListEventsForRun(ctx context.Context, workspaceID, runID uuid.UUID) ([]Event, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_id, run_id, step_id, kind, message, meta_json, created_at
		FROM events WHERE workspace_id=$1 AND run_id=$2
		ORDER BY created_at`, workspaceID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.WorkspaceID, &e.RunID, &e.StepID, &e.Kind, &e.Message, &e.MetaJSON, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListRecentEvents returns recent workspace events for the inbox feed.
func (s *Store) ListRecentEvents(ctx context.Context, workspaceID uuid.UUID, limit int) ([]Event, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_id, run_id, step_id, kind, message, meta_json, created_at
		FROM events WHERE workspace_id=$1
		ORDER BY created_at DESC LIMIT $2`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.WorkspaceID, &e.RunID, &e.StepID, &e.Kind, &e.Message, &e.MetaJSON, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ClaimJob claims the next available job using SKIP LOCKED.
func (s *Store) ClaimJob(ctx context.Context, workerID string, lease time.Duration) (*Job, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Requeue expired claims so crashes do not strand work.
	if _, err := tx.Exec(ctx, `
		UPDATE jobs
		SET status='pending', claimed_by=NULL, claim_expires_at=NULL, updated_at=now()
		WHERE status='claimed' AND claim_expires_at < now()`); err != nil {
		return nil, err
	}

	var j Job
	err = tx.QueryRow(ctx, `
		SELECT id, workspace_id, step_id, job_type, status, available_at, attempts, claimed_by, claim_expires_at
		FROM jobs
		WHERE status='pending' AND available_at <= now()
		ORDER BY available_at
		FOR UPDATE SKIP LOCKED
		LIMIT 1`).Scan(&j.ID, &j.WorkspaceID, &j.StepID, &j.JobType, &j.Status, &j.AvailableAt, &j.Attempts, &j.ClaimedBy, &j.ClaimExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Commit(ctx)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	expires := time.Now().UTC().Add(lease)
	j.Status = "claimed"
	j.Attempts++
	j.ClaimedBy = &workerID
	j.ClaimExpiresAt = &expires

	if _, err := tx.Exec(ctx, `
		UPDATE jobs
		SET status='claimed', attempts=$1, claimed_by=$2, claim_expires_at=$3, updated_at=now()
		WHERE id=$4`, j.Attempts, workerID, expires, j.ID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE run_steps
		SET status='claimed', claimed_by=$1, claim_expires_at=$2, updated_at=now()
		WHERE id=$3`, workerID, expires, j.StepID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &j, nil
}

// StepExecutionContext is everything the worker needs to execute a claimed step.
type StepExecutionContext struct {
	Step       RunStep
	Tool       Tool
	RunID      uuid.UUID
	WorkspaceID uuid.UUID
}

// LoadStepForExecution loads step + tool for a claimed job.
func (s *Store) LoadStepForExecution(ctx context.Context, stepID uuid.UUID) (StepExecutionContext, error) {
	var c StepExecutionContext
	err := s.pool.QueryRow(ctx, `
		SELECT rs.id, rs.run_id, rs.workspace_id, rs.tool_id, t.name, rs.step_index, rs.status,
		       rs.input_json, rs.output_json, rs.error_message, rs.attempt_count, rs.created_at, rs.updated_at,
		       t.id, t.workspace_id, t.name, t.description, t.risk_level, t.requires_approval, t.mock_handler, t.created_at
		FROM run_steps rs
		JOIN tools t ON t.id = rs.tool_id
		WHERE rs.id=$1`, stepID,
	).Scan(
		&c.Step.ID, &c.Step.RunID, &c.Step.WorkspaceID, &c.Step.ToolID, &c.Step.ToolName, &c.Step.StepIndex, &c.Step.Status,
		&c.Step.InputJSON, &c.Step.OutputJSON, &c.Step.ErrorMessage, &c.Step.AttemptCount, &c.Step.CreatedAt, &c.Step.UpdatedAt,
		&c.Tool.ID, &c.Tool.WorkspaceID, &c.Tool.Name, &c.Tool.Description, &c.Tool.RiskLevel, &c.Tool.RequiresApproval, &c.Tool.MockHandler, &c.Tool.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return StepExecutionContext{}, ErrNotFound
	}
	c.RunID = c.Step.RunID
	c.WorkspaceID = c.Step.WorkspaceID
	return c, err
}

// MarkStepSucceeded records a successful tool execution and completes the job/run.
func (s *Store) MarkStepSucceeded(ctx context.Context, jobID, stepID, runID, workspaceID uuid.UUID, attempt int, output json.RawMessage) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO step_attempts (step_id, attempt, status, finished_at, output_json)
		VALUES ($1, $2, 'succeeded', now(), $3)
		ON CONFLICT (step_id, attempt) DO UPDATE
		SET status='succeeded', finished_at=now(), output_json=EXCLUDED.output_json`,
		stepID, attempt, output); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE run_steps
		SET status='succeeded', output_json=$1, attempt_count=$2, claimed_by=NULL, claim_expires_at=NULL, updated_at=now()
		WHERE id=$3`, output, attempt, stepID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE runs
		SET status='succeeded', result_json=$1, updated_at=now()
		WHERE id=$2`, output, runID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE jobs SET status='done', updated_at=now() WHERE id=$1`, jobID); err != nil {
		return err
	}
	if err := appendEventTx(ctx, tx, workspaceID, &runID, &stepID, "step_succeeded", "Tool executed successfully", nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// MarkStepFailed records failure and either schedules retry or fails the run.
func (s *Store) MarkStepFailed(ctx context.Context, jobID, stepID, runID, workspaceID uuid.UUID, attempt, maxAttempts int, errMsg string, backoff time.Duration) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO step_attempts (step_id, attempt, status, finished_at, error_message)
		VALUES ($1, $2, 'failed', now(), $3)
		ON CONFLICT (step_id, attempt) DO UPDATE
		SET status='failed', finished_at=now(), error_message=EXCLUDED.error_message`,
		stepID, attempt, errMsg); err != nil {
		return err
	}

	if attempt >= maxAttempts {
		if _, err := tx.Exec(ctx, `
			UPDATE run_steps
			SET status='failed', error_message=$1, attempt_count=$2, claimed_by=NULL, claim_expires_at=NULL, updated_at=now()
			WHERE id=$3`, errMsg, attempt, stepID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE runs SET status='failed', error_message=$1, updated_at=now() WHERE id=$2`, errMsg, runID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE jobs SET status='dead', last_error=$1, updated_at=now() WHERE id=$2`, errMsg, jobID); err != nil {
			return err
		}
		if err := appendEventTx(ctx, tx, workspaceID, &runID, &stepID, "step_failed", "Tool exhausted retries", map[string]any{"error": errMsg}); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	next := time.Now().UTC().Add(backoff)
	if _, err := tx.Exec(ctx, `
		UPDATE run_steps
		SET status='pending', error_message=$1, attempt_count=$2, next_attempt_at=$3,
		    claimed_by=NULL, claim_expires_at=NULL, updated_at=now()
		WHERE id=$4`, errMsg, attempt, next, stepID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE jobs
		SET status='pending', available_at=$1, claimed_by=NULL, claim_expires_at=NULL, last_error=$2, updated_at=now()
		WHERE id=$3`, next, errMsg, jobID); err != nil {
		return err
	}
	if err := appendEventTx(ctx, tx, workspaceID, &runID, &stepID, "step_retry_scheduled",
		fmt.Sprintf("Retry scheduled (attempt %d/%d)", attempt, maxAttempts), map[string]any{"error": errMsg}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CountDemoUser returns whether the seeded demo user already exists.
func (s *Store) CountDemoUser(ctx context.Context, email string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)`, strings.ToLower(email)).Scan(&ok)
	return ok, err
}
