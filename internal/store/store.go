package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Gentleman-Programming/brain-context/internal/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrConflict     = errors.New("conflict")
	ErrNotFound     = errors.New("not found")
	ErrUnauthorized = errors.New("unauthorized")
)

type contextKey string

const txContextKey contextKey = "store.tx"

type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Store struct {
	pool *pgxpool.Pool
}

type Tenant struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Project struct {
	ID              uuid.UUID `json:"id"`
	TenantID        uuid.UUID `json:"tenant_id"`
	Name            string    `json:"name"`
	RepositoryURL   *string   `json:"repository_url,omitempty"`
	DefaultBranch   string    `json:"default_branch"`
	EmbedModel      string    `json:"embed_model"`
	EmbedDimensions int       `json:"embed_dimensions"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
}

type APIKeyRecord struct {
	ID         uuid.UUID       `json:"id"`
	TenantID   uuid.UUID       `json:"tenant_id"`
	ProjectID  *uuid.UUID      `json:"project_id,omitempty"`
	KeyPrefix  string          `json:"key_prefix"`
	SecretHash string          `json:"-"`
	Scope      auth.TokenScope `json:"scope"`
	ExpiresAt  time.Time       `json:"expires_at"`
	RevokedAt  *time.Time      `json:"revoked_at,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

type CreateProjectParams struct {
	Name            string
	RepositoryURL   *string
	DefaultBranch   string
	EmbedModel      string
	EmbedDimensions int
	EmbedAPIKeyEnc  string // optional: AES-256-GCM encrypted embed API key
}

func New(ctx context.Context, databaseURL string) (*Store, error) {
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		return nil, fmt.Errorf("database url is required")
	}

	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	// Disable TLS for local development if not explicitly configured
	if cfg.ConnConfig.TLSConfig != nil && strings.Contains(databaseURL, "sslmode=disable") {
		cfg.ConnConfig.TLSConfig = nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

func ContextWithTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txContextKey, tx)
}

func TxFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txContextKey).(pgx.Tx)
	return tx, ok
}

func (s *Store) Executor(ctx context.Context) DBTX {
	if tx, ok := TxFromContext(ctx); ok {
		return tx
	}
	return s.pool
}

func SetTenantContext(ctx context.Context, exec DBTX, tenantID uuid.UUID) error {
	var applied string
	if err := exec.QueryRow(ctx, "select set_config('app.tenant_id', $1, true)", tenantID.String()).Scan(&applied); err != nil {
		return fmt.Errorf("set tenant context: %w", err)
	}
	return nil
}

func (s *Store) SetTenantContext(ctx context.Context, exec DBTX, tenantID uuid.UUID) error {
	return SetTenantContext(ctx, exec, tenantID)
}

func (s *Store) CreateTenantWithAPIKey(ctx context.Context, name string, key auth.IssuedToken) (Tenant, APIKeyRecord, error) {
	var tenant Tenant
	var record APIKeyRecord

	err := s.withTx(ctx, func(tx pgx.Tx) error {
		tenant = Tenant{ID: uuid.New(), Name: name}
		if err := tx.QueryRow(ctx, `
			insert into tenants (id, name)
			values ($1, $2)
			returning created_at
		`, tenant.ID, tenant.Name).Scan(&tenant.CreatedAt); err != nil {
			return classifyError(err)
		}

		record = newAPIKeyRecord(tenant.ID, nil, key)
		if err := insertAPIKey(ctx, tx, record); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return Tenant{}, APIKeyRecord{}, err
	}

	return tenant, record, nil
}

func (s *Store) AuthenticateAPIKey(ctx context.Context, rawToken string) (APIKeyRecord, error) {
	parsed, err := auth.ParseToken(rawToken)
	if err != nil {
		return APIKeyRecord{}, ErrUnauthorized
	}

	rows, err := s.pool.Query(ctx, `
		select id, tenant_id, project_id, key_prefix, secret_hash, scope, expires_at, revoked_at, created_at
		from api_keys
		where key_prefix = $1
		order by created_at desc
	`, parsed.KeyPrefix)
	if err != nil {
		return APIKeyRecord{}, fmt.Errorf("query api keys: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		record, scanErr := scanAPIKey(rows)
		if scanErr != nil {
			return APIKeyRecord{}, scanErr
		}
		if record.Scope != parsed.Scope {
			continue
		}
		if err := auth.CompareTokenHash(record.SecretHash, rawToken); err != nil {
			continue
		}
		if record.RevokedAt != nil || time.Now().UTC().After(record.ExpiresAt) {
			return APIKeyRecord{}, ErrUnauthorized
		}
		return record, nil
	}

	if err := rows.Err(); err != nil {
		return APIKeyRecord{}, fmt.Errorf("iterate api keys: %w", err)
	}

	return APIKeyRecord{}, ErrUnauthorized
}

func (s *Store) RenewAPIKey(ctx context.Context, keyID, tenantID uuid.UUID, key auth.IssuedToken) (APIKeyRecord, error) {
	var record APIKeyRecord

	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var projectID pgtype.UUID
		var scope string
		if err := tx.QueryRow(ctx, `
			select project_id, scope
			from api_keys
			where id = $1 and tenant_id = $2 and revoked_at is null
		`, keyID, tenantID).Scan(&projectID, &scope); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("load api key: %w", err)
		}

		if scope != string(key.Scope) {
			return ErrUnauthorized
		}

		if _, err := tx.Exec(ctx, `
			update api_keys
			set revoked_at = $1
			where id = $2 and tenant_id = $3 and revoked_at is null
		`, time.Now().UTC(), keyID, tenantID); err != nil {
			return fmt.Errorf("revoke existing api key: %w", err)
		}

		record = newAPIKeyRecord(tenantID, uuidPtrFromPgType(projectID), key)
		if err := insertAPIKey(ctx, tx, record); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return APIKeyRecord{}, err
	}

	return record, nil
}

func (s *Store) RevokeAPIKey(ctx context.Context, keyID, tenantID uuid.UUID) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		result, err := tx.Exec(ctx, `
			update api_keys
			set revoked_at = now()
			where id = $1 and tenant_id = $2 and revoked_at is null
		`, keyID, tenantID)
		if err != nil {
			return fmt.Errorf("revoke api key: %w", err)
		}
		if result.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (s *Store) CreateProject(ctx context.Context, tenantID uuid.UUID, params CreateProjectParams) (Project, error) {
	var project Project

	err := s.withTx(ctx, func(tx pgx.Tx) error {
		project = Project{
			ID:              uuid.New(),
			TenantID:        tenantID,
			Name:            params.Name,
			RepositoryURL:   params.RepositoryURL,
			DefaultBranch:   params.DefaultBranch,
			EmbedModel:      params.EmbedModel,
			EmbedDimensions: params.EmbedDimensions,
			Status:          "active",
		}

		// Use nullable for embed_api_key_enc
		var embedKeyEnc *string
		if params.EmbedAPIKeyEnc != "" {
			embedKeyEnc = &params.EmbedAPIKeyEnc
		}

		if err := tx.QueryRow(ctx, `
			insert into projects (id, tenant_id, name, repository_url, default_branch, embed_model, embed_dimensions, status, embed_api_key_enc)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			returning created_at
		`, project.ID, project.TenantID, project.Name, project.RepositoryURL, project.DefaultBranch, project.EmbedModel, project.EmbedDimensions, project.Status, embedKeyEnc).Scan(&project.CreatedAt); err != nil {
			return classifyError(err)
		}
		return nil
	})
	if err != nil {
		return Project{}, err
	}

	return project, nil
}

func (s *Store) DeleteProject(ctx context.Context, tenantID, projectID uuid.UUID) error {
	// CASCADE deletes project_files, chunks, vectors, index_jobs, api_keys via FK
	tag, err := s.Executor(ctx).Exec(ctx, `
		DELETE FROM projects
		WHERE id = $1 AND tenant_id = $2
	`, projectID, tenantID)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetProject(ctx context.Context, tenantID, projectID uuid.UUID) (Project, error) {
	var project Project
	var repositoryURL sql.NullString
	err := s.Executor(ctx).QueryRow(ctx, `
		select id, tenant_id, name, repository_url, default_branch, embed_model, embed_dimensions, status, created_at
		from projects
		where id = $1 and tenant_id = $2
	`, projectID, tenantID).Scan(
		&project.ID,
		&project.TenantID,
		&project.Name,
		&repositoryURL,
		&project.DefaultBranch,
		&project.EmbedModel,
		&project.EmbedDimensions,
		&project.Status,
		&project.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Project{}, ErrNotFound
		}
		return Project{}, fmt.Errorf("get project: %w", err)
	}
	if repositoryURL.Valid {
		project.RepositoryURL = &repositoryURL.String
	}
	return project, nil
}

func (s *Store) CreateProjectTokens(ctx context.Context, tenantID, projectID uuid.UUID, projectKey, mcpKey auth.IssuedToken) (APIKeyRecord, APIKeyRecord, error) {
	var projectRecord APIKeyRecord
	var mcpRecord APIKeyRecord

	err := s.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := s.GetProject(ContextWithTx(ctx, tx), tenantID, projectID); err != nil {
			return err
		}

		projectRecord = newAPIKeyRecord(tenantID, &projectID, projectKey)
		if err := insertAPIKey(ctx, tx, projectRecord); err != nil {
			return err
		}

		mcpRecord = newAPIKeyRecord(tenantID, &projectID, mcpKey)
		if err := insertAPIKey(ctx, tx, mcpRecord); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return APIKeyRecord{}, APIKeyRecord{}, err
	}

	return projectRecord, mcpRecord, nil
}

// ListProjectTokens returns active (non-revoked, non-expired) tokens for a project.
// Secrets are never returned.
func (s *Store) ListProjectTokens(ctx context.Context, tenantID, projectID uuid.UUID) ([]APIKeyRecord, error) {
	if _, err := s.GetProject(ctx, tenantID, projectID); err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, project_id, key_prefix, secret_hash, scope, expires_at, revoked_at, created_at
		FROM api_keys
		WHERE tenant_id = $1
		  AND project_id = $2
		  AND revoked_at IS NULL
		  AND expires_at > now()
		  AND scope IN ('project', 'mcp_read')
		ORDER BY created_at DESC
	`, tenantID, projectID)
	if err != nil {
		return nil, fmt.Errorf("query project tokens: %w", err)
	}
	defer rows.Close()

	var records []APIKeyRecord
	for rows.Next() {
		r, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project tokens: %w", err)
	}
	return records, nil
}

// RenewProjectTokens revokes all active project+mcp_read tokens for a project
// and inserts a fresh pair atomically.
func (s *Store) RenewProjectTokens(ctx context.Context, tenantID, projectID uuid.UUID, projectKey, mcpKey auth.IssuedToken) (APIKeyRecord, APIKeyRecord, error) {
	var projectRecord, mcpRecord APIKeyRecord

	err := s.withTx(ctx, func(tx pgx.Tx) error {
		// Verify the project belongs to this tenant
		if _, err := s.GetProject(ContextWithTx(ctx, tx), tenantID, projectID); err != nil {
			return err
		}

		// Revoke all active project + mcp_read tokens for this project
		if _, err := tx.Exec(ctx, `
			UPDATE api_keys
			SET revoked_at = now()
			WHERE tenant_id = $1
			  AND project_id = $2
			  AND scope IN ('project', 'mcp_read')
			  AND revoked_at IS NULL
		`, tenantID, projectID); err != nil {
			return fmt.Errorf("revoke existing project tokens: %w", err)
		}

		// Issue fresh pair
		projectRecord = newAPIKeyRecord(tenantID, &projectID, projectKey)
		if err := insertAPIKey(ctx, tx, projectRecord); err != nil {
			return err
		}

		mcpRecord = newAPIKeyRecord(tenantID, &projectID, mcpKey)
		if err := insertAPIKey(ctx, tx, mcpRecord); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return APIKeyRecord{}, APIKeyRecord{}, err
	}

	return projectRecord, mcpRecord, nil
}

func (s *Store) withTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	if tx, ok := TxFromContext(ctx); ok {
		return fn(tx)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}

func insertAPIKey(ctx context.Context, exec DBTX, record APIKeyRecord) error {
	if _, err := exec.Exec(ctx, `
		insert into api_keys (id, tenant_id, project_id, key_prefix, secret_hash, scope, expires_at, revoked_at, created_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, record.ID, record.TenantID, record.ProjectID, record.KeyPrefix, record.SecretHash, record.Scope, record.ExpiresAt, record.RevokedAt, record.CreatedAt); err != nil {
		return classifyError(err)
	}
	return nil
}

func newAPIKeyRecord(tenantID uuid.UUID, projectID *uuid.UUID, key auth.IssuedToken) APIKeyRecord {
	return APIKeyRecord{
		ID:         uuid.New(),
		TenantID:   tenantID,
		ProjectID:  projectID,
		KeyPrefix:  key.KeyPrefix,
		SecretHash: key.Hash,
		Scope:      key.Scope,
		ExpiresAt:  key.ExpiresAt,
		CreatedAt:  time.Now().UTC(),
	}
}

func scanAPIKey(row interface{ Scan(dest ...any) error }) (APIKeyRecord, error) {
	var record APIKeyRecord
	var scope string
	var projectID pgtype.UUID
	if err := row.Scan(
		&record.ID,
		&record.TenantID,
		&projectID,
		&record.KeyPrefix,
		&record.SecretHash,
		&scope,
		&record.ExpiresAt,
		&record.RevokedAt,
		&record.CreatedAt,
	); err != nil {
		return APIKeyRecord{}, fmt.Errorf("scan api key: %w", err)
	}
	record.ProjectID = uuidPtrFromPgType(projectID)
	record.Scope = auth.TokenScope(scope)
	return record, nil
}

func uuidPtrFromPgType(value pgtype.UUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	parsed := value.Bytes
	result, err := uuid.FromBytes(parsed[:])
	if err != nil {
		return nil
	}
	return &result
}

func classifyError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return err
}

func buildBulkInsertSQL(table string, columns []string, rows int, suffix string) (string, error) {
	if strings.TrimSpace(table) == "" {
		return "", fmt.Errorf("table name is required")
	}
	if len(columns) == 0 {
		return "", fmt.Errorf("at least one column is required")
	}
	if rows <= 0 {
		return "", fmt.Errorf("rows must be greater than zero")
	}

	valueGroups := make([]string, 0, rows)
	parameter := 1
	for row := 0; row < rows; row++ {
		placeholders := make([]string, 0, len(columns))
		for range columns {
			placeholders = append(placeholders, fmt.Sprintf("$%d", parameter))
			parameter++
		}
		valueGroups = append(valueGroups, "("+strings.Join(placeholders, ", ")+")")
	}

	query := fmt.Sprintf("insert into %s (%s) values %s", table, strings.Join(columns, ", "), strings.Join(valueGroups, ", "))
	if trimmed := strings.TrimSpace(suffix); trimmed != "" {
		query += " " + trimmed
	}
	return query, nil
}
