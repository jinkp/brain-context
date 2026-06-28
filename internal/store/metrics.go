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
	TotalMs         int
	EmbedMs         int
	VectorMs        int
	RelationsMs     int
	RerankMs        int
	MetricsMs       int
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
			total_candidates, tokens_returned, tokens_in_repo,
			total_ms, embed_ms, vector_ms, relations_ms, rerank_ms, metrics_ms
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
	`,
		log.TenantID, log.ProjectID, log.ActorPrefix,
		log.ChunksReturned, log.TopScore, log.AvgScore,
		log.TotalCandidates, log.TokensReturned, log.TokensInRepo,
		log.TotalMs, log.EmbedMs, log.VectorMs, log.RelationsMs, log.RerankMs, log.MetricsMs,
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

	// Extended metrics (computed from existing schema)
	QueriesByActor    []ActorBucket    `json:"queries_by_actor"`
	QueriesByHour     []HourBucket     `json:"queries_by_hour"`
	ScoreDistribution ScoreDist        `json:"score_distribution"`
	HitRatio          float64          `json:"hit_ratio"`
	TokensPerQuery    float64          `json:"tokens_per_query"`
	CodebaseStats     []ProjectStats   `json:"codebase_stats"`
	IndexJobs         []IndexJobStats  `json:"index_jobs"`

	// Latency metrics (averages in milliseconds)
	AvgTotalMs     float64 `json:"avg_total_ms"`
	AvgEmbedMs     float64 `json:"avg_embed_ms"`
	AvgVectorMs    float64 `json:"avg_vector_ms"`
	AvgRelationsMs float64 `json:"avg_relations_ms"`
	AvgRerankMs    float64 `json:"avg_rerank_ms"`
	AvgMetricsMs   float64 `json:"avg_metrics_ms"`
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

type ActorBucket struct {
	ActorPrefix string `json:"actor_prefix"`
	Queries     int64  `json:"queries"`
	AvgScore    float64 `json:"avg_score"`
}

type HourBucket struct {
	Hour    int   `json:"hour"`
	Queries int64 `json:"queries"`
}

type ScoreDist struct {
	Excellent int64 `json:"excellent"` // >= 0.7
	Good      int64 `json:"good"`      // 0.4–0.7
	Poor      int64 `json:"poor"`      // < 0.4
}

type ProjectStats struct {
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
	TotalFiles  int64  `json:"total_files"`
	TotalChunks int64  `json:"total_chunks"`
	TotalSizeKB int64  `json:"total_size_kb"`
	Languages   int64  `json:"languages"`
	TopLanguage string `json:"top_language"`
	LastIndexed string `json:"last_indexed"`
}

type IndexJobStats struct {
	ProjectName string `json:"project_name"`
	Kind        string `json:"kind"`
	State       string `json:"state"`
	Attempts    int    `json:"attempts"`
	CreatedAt   string `json:"created_at"`
	FinishedAt  string `json:"finished_at"`
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
			COALESCE(SUM(tokens_in_repo - tokens_returned), 0) as tokens_saved,
			COALESCE(AVG(total_ms), 0)                 as avg_total_ms,
			COALESCE(AVG(embed_ms), 0)                 as avg_embed_ms,
			COALESCE(AVG(vector_ms), 0)                as avg_vector_ms,
			COALESCE(AVG(relations_ms), 0)             as avg_relations_ms,
			COALESCE(AVG(rerank_ms), 0)                as avg_rerank_ms,
			COALESCE(AVG(metrics_ms), 0)               as avg_metrics_ms
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
		&m.AvgTotalMs,
		&m.AvgEmbedMs,
		&m.AvgVectorMs,
		&m.AvgRelationsMs,
		&m.AvgRerankMs,
		&m.AvgMetricsMs,
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

	// ── Extended metrics ─────────────────────────────────────────────────────

	// Queries by actor (who uses brain the most)
	actorRows, err := s.pool.Query(ctx, `
		SELECT
			COALESCE(NULLIF(actor_prefix, ''), '(unknown)') as actor,
			COUNT(*)                     as queries,
			COALESCE(AVG(avg_score), 0)  as avg_score
		FROM query_logs
		WHERE tenant_id = $1
		  AND created_at BETWEEN $2 AND $3
		GROUP BY actor
		ORDER BY queries DESC
		LIMIT 20
	`, tenantID, from, to)
	if err != nil {
		return MetricsSummary{}, fmt.Errorf("get actor metrics: %w", err)
	}
	defer actorRows.Close()
	for actorRows.Next() {
		var b ActorBucket
		if err := actorRows.Scan(&b.ActorPrefix, &b.Queries, &b.AvgScore); err != nil {
			return MetricsSummary{}, fmt.Errorf("scan actor bucket: %w", err)
		}
		m.QueriesByActor = append(m.QueriesByActor, b)
	}

	// Peak usage hours
	hourRows, err := s.pool.Query(ctx, `
		SELECT
			EXTRACT(HOUR FROM created_at AT TIME ZONE 'UTC')::int as hour,
			COUNT(*) as queries
		FROM query_logs
		WHERE tenant_id = $1
		  AND created_at BETWEEN $2 AND $3
		GROUP BY hour
		ORDER BY hour
	`, tenantID, from, to)
	if err != nil {
		return MetricsSummary{}, fmt.Errorf("get hourly metrics: %w", err)
	}
	defer hourRows.Close()
	for hourRows.Next() {
		var b HourBucket
		if err := hourRows.Scan(&b.Hour, &b.Queries); err != nil {
			return MetricsSummary{}, fmt.Errorf("scan hour bucket: %w", err)
		}
		m.QueriesByHour = append(m.QueriesByHour, b)
	}

	// Score distribution: excellent (>=0.7), good (0.4-0.7), poor (<0.4)
	err = s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE top_score >= 0.7)                   as excellent,
			COUNT(*) FILTER (WHERE top_score >= 0.4 AND top_score < 0.7) as good,
			COUNT(*) FILTER (WHERE top_score < 0.4)                    as poor
		FROM query_logs
		WHERE tenant_id = $1
		  AND created_at BETWEEN $2 AND $3
	`, tenantID, from, to).Scan(&m.ScoreDistribution.Excellent, &m.ScoreDistribution.Good, &m.ScoreDistribution.Poor)
	if err != nil {
		return MetricsSummary{}, fmt.Errorf("get score distribution: %w", err)
	}

	// Hit ratio: avg(chunks_returned / total_candidates)
	// Tokens per query: avg(tokens_returned)
	err = s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(AVG(CASE WHEN total_candidates > 0
				THEN chunks_returned::float / total_candidates ELSE 0 END), 0),
			COALESCE(AVG(tokens_returned), 0)
		FROM query_logs
		WHERE tenant_id = $1
		  AND created_at BETWEEN $2 AND $3
	`, tenantID, from, to).Scan(&m.HitRatio, &m.TokensPerQuery)
	if err != nil {
		return MetricsSummary{}, fmt.Errorf("get hit ratio: %w", err)
	}

	// Codebase stats per project (uses RLS-free direct query since we filter by tenant_id)
	codebaseRows, err := s.pool.Query(ctx, `
		SELECT
			p.id::text,
			p.name,
			COUNT(DISTINCT f.id)             as total_files,
			COUNT(DISTINCT c.id)             as total_chunks,
			COALESCE(SUM(f.size_bytes) / 1024, 0) as total_size_kb,
			COUNT(DISTINCT f.language)       as languages,
			(SELECT ff.language FROM project_files ff
			 WHERE ff.project_id = p.id
			 GROUP BY ff.language ORDER BY COUNT(*) DESC LIMIT 1) as top_language,
			COALESCE(TO_CHAR(MAX(f.indexed_at) AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI'), 'never') as last_indexed
		FROM projects p
		LEFT JOIN project_files f ON f.project_id = p.id
		LEFT JOIN chunks c ON c.project_id = p.id
		WHERE p.tenant_id = $1 AND p.status = 'active'
		GROUP BY p.id, p.name
		ORDER BY total_chunks DESC
	`, tenantID)
	if err != nil {
		return MetricsSummary{}, fmt.Errorf("get codebase stats: %w", err)
	}
	defer codebaseRows.Close()
	for codebaseRows.Next() {
		var ps ProjectStats
		var topLang *string
		if err := codebaseRows.Scan(&ps.ProjectID, &ps.ProjectName, &ps.TotalFiles,
			&ps.TotalChunks, &ps.TotalSizeKB, &ps.Languages, &topLang, &ps.LastIndexed); err != nil {
			return MetricsSummary{}, fmt.Errorf("scan codebase stats: %w", err)
		}
		if topLang != nil {
			ps.TopLanguage = *topLang
		}
		m.CodebaseStats = append(m.CodebaseStats, ps)
	}

	// Recent index jobs (last 10)
	jobRows, err := s.pool.Query(ctx, `
		SELECT
			p.name,
			j.kind,
			j.state,
			j.attempt,
			TO_CHAR(j.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI') as created,
			COALESCE(TO_CHAR(j.finished_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI'), 'running') as finished
		FROM index_jobs j
		JOIN projects p ON p.id = j.project_id
		WHERE j.tenant_id = $1
		ORDER BY j.created_at DESC
		LIMIT 10
	`, tenantID)
	if err != nil {
		return MetricsSummary{}, fmt.Errorf("get index jobs: %w", err)
	}
	defer jobRows.Close()
	for jobRows.Next() {
		var j IndexJobStats
		if err := jobRows.Scan(&j.ProjectName, &j.Kind, &j.State, &j.Attempts, &j.CreatedAt, &j.FinishedAt); err != nil {
			return MetricsSummary{}, fmt.Errorf("scan index job: %w", err)
		}
		m.IndexJobs = append(m.IndexJobs, j)
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
