package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Gentleman-Programming/brain-context/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Job struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	ProjectID      uuid.UUID
	Kind           string
	State          string
	Priority       int16
	Attempt        int
	MaxAttempts    int
	LeaseExpiresAt *time.Time
	Error          *string
	CreatedAt      time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
}

type CreateJobParams struct {
	TenantID    uuid.UUID
	ProjectID   uuid.UUID
	Kind        string
	Priority    int16
	MaxAttempts int
	RequestedBy *uuid.UUID
}

type jobTransition struct {
	NextState   string
	NextAttempt int
}

func CreateJob(ctx context.Context, st *store.Store, params CreateJobParams) (Job, error) {
	if params.Kind == "" {
		return Job{}, fmt.Errorf("job kind is required")
	}
	if params.MaxAttempts <= 0 {
		params.MaxAttempts = 5
	}
	job := Job{
		ID:          uuid.New(),
		TenantID:    params.TenantID,
		ProjectID:   params.ProjectID,
		Kind:        params.Kind,
		State:       "queued",
		Priority:    params.Priority,
		MaxAttempts: params.MaxAttempts,
	}

	err := st.Executor(ctx).QueryRow(ctx, `
		insert into index_jobs (id, tenant_id, project_id, kind, state, priority, max_attempts, requested_by)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
		returning attempt, created_at
	`, job.ID, job.TenantID, job.ProjectID, job.Kind, job.State, job.Priority, job.MaxAttempts, params.RequestedBy).Scan(&job.Attempt, &job.CreatedAt)
	if err != nil {
		return Job{}, fmt.Errorf("create job: %w", err)
	}
	return job, nil
}

func LeaseJob(ctx context.Context, st *store.Store, tenantID uuid.UUID) (Job, error) {
	var job Job
	var leaseExpiresAt, startedAt, finishedAt *time.Time
	var jobError *string

	err := st.Executor(ctx).QueryRow(ctx, `
		with recovered as (
			update index_jobs
			set state = 'queued', lease_expires_at = null
			where tenant_id = $1 and state = 'running' and lease_expires_at < now()
		), next_job as (
			select id
			from index_jobs
			where tenant_id = $1 and state in ('queued', 'retry')
			order by priority asc, created_at asc
			limit 1
			for update skip locked
		)
		update index_jobs j
		set state = 'running',
			lease_expires_at = now() + interval '5 minutes',
			started_at = coalesce(j.started_at, now())
		from next_job
		where j.id = next_job.id
		returning j.id, j.tenant_id, j.project_id, j.kind, j.state, j.priority, j.attempt,
			j.max_attempts, j.lease_expires_at, j.error, j.created_at, j.started_at, j.finished_at
	`, tenantID).Scan(
		&job.ID,
		&job.TenantID,
		&job.ProjectID,
		&job.Kind,
		&job.State,
		&job.Priority,
		&job.Attempt,
		&job.MaxAttempts,
		&leaseExpiresAt,
		&jobError,
		&job.CreatedAt,
		&startedAt,
		&finishedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, store.ErrNotFound
		}
		return Job{}, fmt.Errorf("lease job: %w", err)
	}
	job.LeaseExpiresAt = leaseExpiresAt
	job.Error = jobError
	job.StartedAt = startedAt
	job.FinishedAt = finishedAt
	return job, nil
}

func HeartbeatJob(ctx context.Context, st *store.Store, jobID uuid.UUID) error {
	result, err := st.Executor(ctx).Exec(ctx, `
		update index_jobs
		set lease_expires_at = now() + interval '5 minutes'
		where id = $1 and state = 'running'
	`, jobID)
	if err != nil {
		return fmt.Errorf("heartbeat job: %w", err)
	}
	if result.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func CompleteJob(ctx context.Context, st *store.Store, jobID uuid.UUID) error {
	result, err := st.Executor(ctx).Exec(ctx, `
		update index_jobs
		set state = 'done', lease_expires_at = null, error = null, finished_at = now()
		where id = $1
	`, jobID)
	if err != nil {
		return fmt.Errorf("complete job: %w", err)
	}
	if result.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func FailJob(ctx context.Context, st *store.Store, jobID uuid.UUID, message string) error {
	result, err := st.Executor(ctx).Exec(ctx, `
		update index_jobs
		set state = case when attempt + 1 < max_attempts then 'retry' else 'failed' end,
			attempt = attempt + 1,
			lease_expires_at = null,
			error = $2,
			finished_at = case when attempt + 1 < max_attempts then null else now() end
		where id = $1
	`, jobID, message)
	if err != nil {
		return fmt.Errorf("fail job: %w", err)
	}
	if result.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func failTransition(attempt, maxAttempts int) jobTransition {
	nextAttempt := attempt + 1
	state := "retry"
	if nextAttempt >= maxAttempts {
		state = "failed"
	}
	return jobTransition{
		NextState:   state,
		NextAttempt: nextAttempt,
	}
}

func validStateTransition(from, to string) bool {
	switch from {
	case "queued":
		return to == "running"
	case "running":
		return to == "done" || to == "retry" || to == "failed"
	case "retry":
		return to == "queued"
	default:
		return false
	}
}
