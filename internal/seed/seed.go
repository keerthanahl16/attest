// Package seed creates a demo workspace so the inbox is usable on first boot.
package seed

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/keerthanahl16/attest/internal/auth"
	"github.com/keerthanahl16/attest/internal/store"
)

const (
	DemoEmail    = "demo@attest.dev"
	DemoPassword = "AttestDemo!123"
	DemoName     = "Demo Approver"
)

// Result is printed once so local demos can log in immediately.
type Result struct {
	Email     string
	Password  string
	Workspace store.Workspace
	APIKey    string
}

// EnsureDemo creates the demo user/workspace/tools/API key if missing.
func EnsureDemo(ctx context.Context, st *store.Store, log *slog.Logger) (*Result, error) {
	exists, err := st.CountDemoUser(ctx, DemoEmail)
	if err != nil {
		return nil, err
	}
	if exists {
		log.Info("demo user already present", "email", DemoEmail)
		return nil, nil
	}

	hash, err := auth.HashPassword(DemoPassword)
	if err != nil {
		return nil, err
	}
	user, err := st.CreateUser(ctx, DemoEmail, DemoName, hash)
	if err != nil {
		return nil, err
	}
	ws, err := st.CreateWorkspaceWithOwner(ctx, "Demo Workspace", "demo", user.ID)
	if err != nil {
		return nil, err
	}

	_, err = st.CreateTool(ctx, store.Tool{
		WorkspaceID:      ws.ID,
		Name:             "issue_refund",
		Description:      "Posts a mock refund. Requires human approval because money movement is irreversible.",
		RiskLevel:        "irreversible",
		RequiresApproval: true,
		MockHandler:      "issue_refund",
	})
	if err != nil {
		return nil, err
	}
	_, err = st.CreateTool(ctx, store.Tool{
		WorkspaceID:      ws.ID,
		Name:             "send_email",
		Description:      "Sends a mock notification email. Safe; runs without approval.",
		RiskLevel:        "safe",
		RequiresApproval: false,
		MockHandler:      "send_email",
	})
	if err != nil {
		return nil, err
	}

	// Fixed local-only key so README / UI demos stay copy-pasteable. Never reuse in production.
	apiKey := "atk_demo_local_only_do_not_use_in_prod"
	prefix := apiKey[:12]
	meta, err := st.CreateAPIKey(ctx, ws.ID, "demo-agent", prefix, auth.HashToken(apiKey))
	if err != nil {
		return nil, err
	}

	log.Info("seeded demo workspace",
		"email", DemoEmail,
		"workspace", ws.Slug,
		"api_key_prefix", meta.KeyPrefix,
	)
	fmt.Println("====================================================")
	fmt.Println(" Attest demo credentials")
	fmt.Println(" Email:    ", DemoEmail)
	fmt.Println(" Password: ", DemoPassword)
	fmt.Println(" API key:  ", apiKey)
	fmt.Println("====================================================")

	return &Result{
		Email:     DemoEmail,
		Password:  DemoPassword,
		Workspace: ws,
		APIKey:    apiKey,
	}, nil
}
