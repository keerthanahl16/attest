package store

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// User is a human who signs into the approval inbox.
type User struct {
	ID           uuid.UUID `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

// Workspace is a tenant that owns tools, API keys, and runs.
type Workspace struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
}

// APIKeyMeta is the non-secret view of an API key.
type APIKeyMeta struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"`
	CreatedAt  time.Time  `json:"created_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

// Tool is a registered capability an agent may invoke.
type Tool struct {
	ID                uuid.UUID `json:"id"`
	WorkspaceID       uuid.UUID `json:"workspace_id"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	RiskLevel         string    `json:"risk_level"`
	RequiresApproval  bool      `json:"requires_approval"`
	MockHandler       string    `json:"mock_handler"`
	CreatedAt         time.Time `json:"created_at"`
}

// Run is one idempotent agent execution request.
type Run struct {
	ID              uuid.UUID       `json:"id"`
	WorkspaceID     uuid.UUID       `json:"workspace_id"`
	IdempotencyKey  string          `json:"idempotency_key"`
	Status          string          `json:"status"`
	InputJSON       json.RawMessage `json:"input"`
	ResultJSON      json.RawMessage `json:"result,omitempty"`
	ErrorMessage    *string         `json:"error_message,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	Steps           []RunStep       `json:"steps,omitempty"`
}

// RunStep is one tool invocation inside a run.
type RunStep struct {
	ID           uuid.UUID       `json:"id"`
	RunID        uuid.UUID       `json:"run_id"`
	WorkspaceID  uuid.UUID       `json:"workspace_id"`
	ToolID       uuid.UUID       `json:"tool_id"`
	ToolName     string          `json:"tool_name,omitempty"`
	StepIndex    int             `json:"step_index"`
	Status       string          `json:"status"`
	InputJSON    json.RawMessage `json:"input"`
	OutputJSON   json.RawMessage `json:"output,omitempty"`
	ErrorMessage *string         `json:"error_message,omitempty"`
	AttemptCount int             `json:"attempt_count"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// Approval is a human gate on an irreversible step.
type Approval struct {
	ID           uuid.UUID  `json:"id"`
	StepID       uuid.UUID  `json:"step_id"`
	RunID        uuid.UUID  `json:"run_id"`
	WorkspaceID  uuid.UUID  `json:"workspace_id"`
	Status       string     `json:"status"`
	RequestedAt  time.Time  `json:"requested_at"`
	DecidedAt    *time.Time `json:"decided_at,omitempty"`
	DecidedBy    *uuid.UUID `json:"decided_by,omitempty"`
	Reason       *string    `json:"reason,omitempty"`
	ToolName     string     `json:"tool_name,omitempty"`
	StepInput    json.RawMessage `json:"step_input,omitempty"`
	RunIdemKey   string     `json:"idempotency_key,omitempty"`
}

// Event is one timeline entry for the inbox UI.
type Event struct {
	ID          uuid.UUID       `json:"id"`
	WorkspaceID uuid.UUID       `json:"workspace_id"`
	RunID       *uuid.UUID      `json:"run_id,omitempty"`
	StepID      *uuid.UUID      `json:"step_id,omitempty"`
	Kind        string          `json:"kind"`
	Message     string          `json:"message"`
	MetaJSON    json.RawMessage `json:"meta,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

// Job is a queue row claimed by a worker via SKIP LOCKED.
type Job struct {
	ID             uuid.UUID
	WorkspaceID    uuid.UUID
	StepID         uuid.UUID
	JobType        string
	Status         string
	AvailableAt    time.Time
	Attempts       int
	ClaimedBy      *string
	ClaimExpiresAt *time.Time
}
