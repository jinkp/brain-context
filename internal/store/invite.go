package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/Gentleman-Programming/brain-context/internal/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// pgxTx is an alias so invite.go can use pgx.Tx without importing pgx directly.
type pgxTx = pgx.Tx

// InviteCode represents a short-lived token that lets a developer
// join a project and download its config (including the encrypted embed key).
type InviteCode struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	ProjectID uuid.UUID
	Code      string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// ProjectJoinConfig is the payload returned when redeeming an invite code.
// It contains everything a developer needs to configure their local CLI.
type ProjectJoinConfig struct {
	ProjectID       string
	ProjectName     string
	EmbedModel      string
	EmbedDimensions int
	EmbedAPIKeyEnc  string // AES-256-GCM encrypted, hex-encoded
	MCPReadKey      string // raw token — shown once on join
	MCPReadKeyID    string
}

// SaveEmbedAPIKey stores the encrypted embed API key for a project.
// Uses a transaction with SET LOCAL to satisfy RLS on the projects table.
func (s *Store) SaveEmbedAPIKey(ctx context.Context, tenantID, projectID uuid.UUID, encryptedKey string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx for save embed key: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID.String()); err != nil {
		return fmt.Errorf("set tenant context: %w", err)
	}

	result, err := tx.Exec(ctx, `
		UPDATE projects
		SET embed_api_key_enc = $1
		WHERE id = $2 AND tenant_id = $3
	`, encryptedKey, projectID, tenantID)
	if err != nil {
		return fmt.Errorf("save embed api key: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("project not found or not owned by tenant")
	}

	return tx.Commit(ctx)
}

// GetEmbedAPIKey returns the encrypted embed API key for a project.
func (s *Store) GetEmbedAPIKey(ctx context.Context, tenantID, projectID uuid.UUID) (string, error) {
	var enc *string
	err := s.pool.QueryRow(ctx, `
		SELECT embed_api_key_enc FROM projects
		WHERE id = $1 AND tenant_id = $2
	`, projectID, tenantID).Scan(&enc)
	if err != nil {
		return "", fmt.Errorf("get embed api key: %w", err)
	}
	if enc == nil || *enc == "" {
		return "", fmt.Errorf("no embed api key configured for this project — run `brain register` with --embed-api-key")
	}
	return *enc, nil
}

// CreateInviteCode generates a new invite code for a project valid for ttl duration.
func (s *Store) CreateInviteCode(ctx context.Context, tenantID, projectID uuid.UUID, ttl time.Duration) (InviteCode, error) {
	// Verify project belongs to tenant
	if _, err := s.GetProject(ctx, tenantID, projectID); err != nil {
		return InviteCode{}, err
	}

	code, err := generateInviteCode()
	if err != nil {
		return InviteCode{}, err
	}

	invite := InviteCode{
		ID:        uuid.New(),
		TenantID:  tenantID,
		ProjectID: projectID,
		Code:      code,
		ExpiresAt: time.Now().UTC().Add(ttl),
		CreatedAt: time.Now().UTC(),
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO invite_codes (id, tenant_id, project_id, code, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, invite.ID, invite.TenantID, invite.ProjectID, invite.Code, invite.ExpiresAt, invite.CreatedAt)
	if err != nil {
		return InviteCode{}, fmt.Errorf("create invite code: %w", err)
	}

	return invite, nil
}

// RedeemInviteCode validates the code, marks it used, issues a fresh mcp_read key,
// and returns all config the developer needs.
func (s *Store) RedeemInviteCode(ctx context.Context, code string) (ProjectJoinConfig, error) {
	return s.redeemInvite(ctx, code)
}

func (s *Store) redeemInvite(ctx context.Context, code string) (ProjectJoinConfig, error) {
	// Step 1: Read invite without RLS (invite_codes has no RLS policy)
	var invite InviteCode
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, project_id, expires_at, used_at
		FROM invite_codes
		WHERE code = $1
	`, code).Scan(&invite.ID, &invite.TenantID, &invite.ProjectID, &invite.ExpiresAt, &invite.UsedAt)
	if err != nil {
		return ProjectJoinConfig{}, ErrNotFound
	}
	if invite.UsedAt != nil {
		return ProjectJoinConfig{}, fmt.Errorf("invite code already used")
	}
	if time.Now().UTC().After(invite.ExpiresAt) {
		return ProjectJoinConfig{}, fmt.Errorf("invite code expired")
	}

	// Step 2: Read project directly using tenant filter (bypass RLS with explicit WHERE)
	var projectName, embedModel string
	var embedDimensions int
	var embedKeyEnc *string

	err = s.pool.QueryRow(ctx, `
		SELECT name, embed_model, embed_dimensions, embed_api_key_enc
		FROM projects
		WHERE id = $1 AND tenant_id = $2
	`, invite.ProjectID, invite.TenantID).Scan(
		&projectName, &embedModel, &embedDimensions, &embedKeyEnc,
	)
	if err != nil {
		return ProjectJoinConfig{}, fmt.Errorf("load project: %w", err)
	}
	if embedKeyEnc == nil || *embedKeyEnc == "" {
		return ProjectJoinConfig{}, fmt.Errorf("project has no embed API key — ask your admin to run: brain register --project <name> --embed-api-key <key>")
	}

	// Step 3: Mark invite used + issue mcp_read key atomically
	var result ProjectJoinConfig

	err = s.withTx(ctx, func(tx pgxTx) error {
		// Re-check used_at inside tx to prevent races
		var usedAt *time.Time
		if err := tx.QueryRow(ctx,
			`SELECT used_at FROM invite_codes WHERE id = $1 FOR UPDATE`, invite.ID,
		).Scan(&usedAt); err != nil {
			return ErrNotFound
		}
		if usedAt != nil {
			return fmt.Errorf("invite code already used")
		}

		if _, err := tx.Exec(ctx,
			`UPDATE invite_codes SET used_at = now() WHERE id = $1`, invite.ID,
		); err != nil {
			return fmt.Errorf("mark invite used: %w", err)
		}

		// Set tenant context for api_keys INSERT (RLS)
		if _, err := tx.Exec(ctx,
			`SELECT set_config('app.tenant_id', $1, true)`, invite.TenantID.String(),
		); err != nil {
			return fmt.Errorf("set tenant context: %w", err)
		}

		mcpIssued, err := auth.IssueToken(auth.ScopeMCPRead, time.Now().UTC())
		if err != nil {
			return fmt.Errorf("issue mcp read key: %w", err)
		}
		mcpRecord := newAPIKeyRecord(invite.TenantID, &invite.ProjectID, mcpIssued)
		if err := insertAPIKey(ctx, tx, mcpRecord); err != nil {
			return err
		}

		result = ProjectJoinConfig{
			ProjectID:       invite.ProjectID.String(),
			ProjectName:     projectName,
			EmbedModel:      embedModel,
			EmbedDimensions: embedDimensions,
			EmbedAPIKeyEnc:  *embedKeyEnc,
			MCPReadKey:      mcpIssued.Raw,
			MCPReadKeyID:    mcpRecord.ID.String(),
		}
		return nil
	})
	if err != nil {
		return ProjectJoinConfig{}, err
	}

	return result, nil
}

// generateInviteCode returns a random 24-char hex string prefixed with brn_invite_.
func generateInviteCode() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate invite code: %w", err)
	}
	return "brn_invite_" + hex.EncodeToString(b), nil
}
