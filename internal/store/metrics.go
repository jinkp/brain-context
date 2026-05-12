package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)


// QueryLog records one MCP/API search call for usage analytics.
type QueryLog struct {
	TenantID        uuid.UUID
	ProjectID       uuid.UUID
	ActorPrefix     string
	ChunksReturned  int
	TopScore        float32
	AvgScore        float32
	TotalCandidates int
	TokensReturned  int
	TokensInRepo    int
}

// InsertQueryLog persists a search event using a fresh background context
// so it is never affected by request cancellation or tenant-scoped connections.
func (s *Store) InsertQueryLog(_ context.Context, log QueryLog) {
	ctx := context.Background()
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		fmt.Printf("[metrics] acquire conn failed: %v\n", err)
		return
	}
	defer conn.Release()

	_, err = conn.Exec(ctx, `
		INSERT INTO query_logs (
			tenant_id, project_id, actor_prefix,
			chunks_returned, top_score, avg_score,
			total_candidates, tokens_returned, tokens_in_repo
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`,
		log.TenantID, log.ProjectID, log.ActorPrefix,
		log.ChunksReturned, log.TopScore, log.AvgScore,
		log.TotalCandidates, log.TokensReturned, log.TokensInRepo,
	)
	if err != nil {
		fmt.Printf("[metrics] insert query_log failed: %v\n", err)
	}
}

// ── Metrics types ─────────────────────────────────────────────────────────────

type MetricsSummary struct {
	TotalQueries          int64          `json:"total_queries"`
	QueriesLast7Days      int64          `json:"queries_last_7_days"`
	QueriesLast30Days     int64          `json:"queries_last_30_days"`
	AvgScore              float64        `json:"avg_score"`
	AvgChunksReturned     float64        `json:"avg_chunks_returned"`
	EstimatedTokensSaved  int64          `json:"estimated_tokens_saved"`
	EstimatedTokensServed int64          `json:"estimated_tokens_served"`
	SavingsPercent        float64        `json:"savings_percent"`
	QueriesByDay          []DayBucket    `json:"queries_by_day"`
	QueriesByProject      []ProjectBucket `json:"queries_by_project"`
}

type DayBucket struct {
	Date    string `json:"date"`
	Queries int64  `json:"queries"`
}

type ProjectBucket struct {
	ProjectID   string  `json:"project_id"`
	ProjectName string  `json:"project_name"`
	Queries     int64   `json:"queries"`
	AvgScore    float64 `json:"avg_score"`
}

// GetMetrics returns usage analytics for a tenant.
func (s *Store) GetMetrics(ctx context.Context, tenantID uuid.UUID, from, to time.Time) (MetricsSummary, error) {
	var m MetricsSummary

	// Total queries in range
	err := s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*)                                    as total,
			COUNT(*) FILTER (WHERE created_at >= now() - interval '7 days')  as last_7,
			COUNT(*) FILTER (WHERE created_at >= now() - interval '30 days') as last_30,
			COALESCE(AVG(avg_score), 0)                as avg_score,
			COALESCE(AVG(chunks_returned), 0)          as avg_chunks,
			COALESCE(SUM(tokens_returned), 0)          as tokens_served,
			COALESCE(SUM(tokens_in_repo - tokens_returned), 0) as tokens_saved
		FROM query_logs
		WHERE tenant_id = $1
		  AND created_at BETWEEN $2 AND $3
	`, tenantID, from, to).Scan(
		&m.TotalQueries,
		&m.QueriesLast7Days,
		&m.QueriesLast30Days,
		&m.AvgScore,
		&m.AvgChunksReturned,
		&m.EstimatedTokensServed,
		&m.EstimatedTokensSaved,
	)
	if err != nil {
		return MetricsSummary{}, fmt.Errorf("get metrics summary: %w", err)
	}

	// Savings percent
	total := m.EstimatedTokensSaved + m.EstimatedTokensServed
	if total > 0 {
		m.SavingsPercent = float64(m.EstimatedTokensSaved) / float64(total) * 100
	}

	// Queries by day (last 30 days within range)
	rows, err := s.pool.Query(ctx, `
		SELECT
			TO_CHAR(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD') as day,
			COUNT(*) as queries
		FROM query_logs
		WHERE tenant_id = $1
		  AND created_at BETWEEN $2 AND $3
		GROUP BY day
		ORDER BY day
	`, tenantID, from, to)
	if err != nil {
		return MetricsSummary{}, fmt.Errorf("get daily metrics: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var b DayBucket
		if err := rows.Scan(&b.Date, &b.Queries); err != nil {
			return MetricsSummary{}, fmt.Errorf("scan day bucket: %w", err)
		}
		m.QueriesByDay = append(m.QueriesByDay, b)
	}

	// Queries by project
	projectRows, err := s.pool.Query(ctx, `
		SELECT
			q.project_id::text,
			p.name,
			COUNT(*)                    as queries,
			COALESCE(AVG(q.avg_score), 0) as avg_score
		FROM query_logs q
		JOIN projects p ON p.id = q.project_id
		WHERE q.tenant_id = $1
		  AND q.created_at BETWEEN $2 AND $3
		GROUP BY q.project_id, p.name
		ORDER BY queries DESC
		LIMIT 20
	`, tenantID, from, to)
	if err != nil {
		return MetricsSummary{}, fmt.Errorf("get project metrics: %w", err)
	}
	defer projectRows.Close()
	for projectRows.Next() {
		var b ProjectBucket
		if err := projectRows.Scan(&b.ProjectID, &b.ProjectName, &b.Queries, &b.AvgScore); err != nil {
			return MetricsSummary{}, fmt.Errorf("scan project bucket: %w", err)
		}
		m.QueriesByProject = append(m.QueriesByProject, b)
	}

	return m, nil
}

// GetProjectChunkCount returns total chunks and avg token count for a project.
// Uses tenant_id to satisfy RLS policies.
func (s *Store) GetProjectChunkCount(ctx context.Context, tenantID, projectID uuid.UUID) (count int, avgTokens int, err error) {
	// Use a transaction to set the tenant context for RLS
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 150, nil // non-fatal, use default
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID.String()); err != nil {
		return 0, 150, nil
	}

	err = tx.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(AVG(token_count)::int, 150)
		FROM chunks
		WHERE project_id = $1
	`, projectID).Scan(&count, &avgTokens)
	if err != nil {
		return 0, 150, nil
	}
	return count, avgTokens, nil
}
