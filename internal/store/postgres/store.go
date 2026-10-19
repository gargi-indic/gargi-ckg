package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gargi-indic/gargi-ckg/pkg/engine"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	_ engine.StateStore     = (*Store)(nil)
	_ engine.LockManager    = (*Store)(nil)
	_ engine.ExecutionLog   = (*Store)(nil)
	_ engine.EventPublisher = (*Store)(nil)
)

// Config configures the PostgreSQL state store.
type Config struct {
	// ConnString is the PostgreSQL connection string or URL.
	ConnString string
	// PoolConfig is an optional custom pgxpool configuration.
	PoolConfig *pgxpool.Config
	// ExistingPool allows injecting an existing pgxpool.Pool instance.
	ExistingPool *pgxpool.Pool
	// Schema optionally specifies a PostgreSQL schema for multi-tenant or test isolation.
	Schema string
	// AutoMigrate runs embedded database migrations automatically upon store creation.
	// Defaults to true when using New or NewStore.
	AutoMigrate bool
}

// ExecutionFilter defines filtering parameters for QueryExecutions.
type ExecutionFilter struct {
	GraphID   string
	Status    engine.Status
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Offset    int
}

// ExecutionSummary represents summary metadata for an execution in QueryExecutions.
type ExecutionSummary struct {
	ExecutionID engine.ExecutionID `json:"executionId"`
	GraphID     string             `json:"graphId"`
	Status      engine.Status      `json:"status"`
	Version     uint64             `json:"version"`
	CreatedAt   time.Time          `json:"createdAt"`
	UpdatedAt   time.Time          `json:"updatedAt"`
}

// NodeStateRecord represents an individual node execution state record in QueryNodeStates.
type NodeStateRecord struct {
	ExecutionID  engine.ExecutionID `json:"executionId"`
	NodeID       engine.NodeID      `json:"nodeId"`
	Status       engine.Status      `json:"status"`
	Version      uint64             `json:"version"`
	Output       map[string]any     `json:"output"`
	ErrorMessage string             `json:"errorMessage"`
	UpdatedAt    time.Time          `json:"updatedAt"`
}

// Store is a PostgreSQL-backed implementation of engine.StateStore,
// engine.LockManager, engine.ExecutionLog, and engine.EventPublisher.
type Store struct {
	pool      *pgxpool.Pool
	migrator  *Migrator
	ownsPool  bool
	schema    string
	closed    atomic.Bool
}

// New creates and initializes a new PostgreSQL Store.
func New(ctx context.Context, cfg Config) (*Store, error) {
	if cfg.ExistingPool == nil && cfg.PoolConfig == nil && strings.TrimSpace(cfg.ConnString) == "" {
		return nil, errors.New("postgres: connection string, pool config, or existing pool must be provided")
	}

	var pool *pgxpool.Pool
	ownsPool := false

	if cfg.ExistingPool != nil {
		pool = cfg.ExistingPool
	} else {
		poolConfig := cfg.PoolConfig
		if poolConfig == nil {
			var err error
			poolConfig, err = pgxpool.ParseConfig(cfg.ConnString)
			if err != nil {
				return nil, fmt.Errorf("postgres: invalid connection config: %w", err)
			}
		}

		if cfg.Schema != "" {
			// Connect temporarily to ensure the schema exists
			tempPool, err := pgxpool.NewWithConfig(ctx, poolConfig)
			if err != nil {
				return nil, wrapUnavailable(err)
			}
			_, err = tempPool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", pgx.Identifier{cfg.Schema}.Sanitize()))
			tempPool.Close()
			if err != nil {
				return nil, fmt.Errorf("postgres: create schema %s: %w", cfg.Schema, err)
			}

			if poolConfig.ConnConfig.RuntimeParams == nil {
				poolConfig.ConnConfig.RuntimeParams = make(map[string]string)
			}
			poolConfig.ConnConfig.RuntimeParams["search_path"] = fmt.Sprintf("%s, public", cfg.Schema)
		}

		var err error
		pool, err = pgxpool.NewWithConfig(ctx, poolConfig)
		if err != nil {
			return nil, wrapUnavailable(err)
		}
		ownsPool = true
	}

	// Verify database connectivity
	if err := pool.Ping(ctx); err != nil {
		if ownsPool {
			pool.Close()
		}
		return nil, wrapUnavailable(err)
	}

	migrator, err := NewMigrator(pool)
	if err != nil {
		if ownsPool {
			pool.Close()
		}
		return nil, fmt.Errorf("postgres: failed to initialize migrator: %w", err)
	}

	// AutoMigrate is enabled by default unless explicitly disabled
	if cfg.AutoMigrate {
		if err := migrator.Up(ctx); err != nil {
			if ownsPool {
				pool.Close()
			}
			return nil, fmt.Errorf("postgres: migration up failed: %w", err)
		}
	}

	return &Store{
		pool:     pool,
		migrator: migrator,
		ownsPool: ownsPool,
		schema:   cfg.Schema,
	}, nil
}

// NewStore is an alias for New, constructing a new PostgreSQL Store.
func NewStore(ctx context.Context, cfg Config) (*Store, error) {
	return New(ctx, cfg)
}

// Close closes the underlying connection pool if owned by this Store.
func (s *Store) Close() {
	if s.closed.CompareAndSwap(false, true) {
		if s.ownsPool && s.pool != nil {
			s.pool.Close()
		}
	}
}

// Pool returns the underlying pgxpool.Pool instance.
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

// MigrateUp executes all pending embedded migrations up.
func (s *Store) MigrateUp(ctx context.Context) error {
	if s.closed.Load() {
		return s.errClosed()
	}
	return s.migrator.Up(ctx)
}

// MigrateDown rolls back embedded migrations. If all is true, rolls back all versions.
func (s *Store) MigrateDown(ctx context.Context, all bool) error {
	if s.closed.Load() {
		return s.errClosed()
	}
	return s.migrator.Down(ctx, all)
}

// MigrationVersion returns the current schema migration version and dirty status.
func (s *Store) MigrationVersion(ctx context.Context) (int64, bool, error) {
	if s.closed.Load() {
		return 0, false, s.errClosed()
	}
	return s.migrator.Version(ctx)
}

// GetState retrieves the current GraphState for the given executionID.
func (s *Store) GetState(ctx context.Context, executionID engine.ExecutionID) (*engine.GraphState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.closed.Load() {
		return nil, s.errClosed()
	}
	if executionID == "" {
		return nil, errors.New("store: execution ID cannot be empty")
	}

	var stateJSON []byte
	err := s.pool.QueryRow(ctx, "SELECT state_json FROM executions WHERE execution_id = $1", string(executionID)).Scan(&stateJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &engine.StoreNotFoundError{
				ExecutionID: executionID,
				Entity:      "GraphState",
				Err:         engine.ErrStoreNotFound,
			}
		}
		return nil, s.wrapErr(err)
	}

	var state engine.GraphState
	if err := json.Unmarshal(stateJSON, &state); err != nil {
		return nil, fmt.Errorf("postgres: failed to decode state json: %w", err)
	}

	return &state, nil
}

// SetState stores or updates the entire GraphState enforcing optimistic concurrency against expectedVersion.
func (s *Store) SetState(ctx context.Context, executionID engine.ExecutionID, state *engine.GraphState, expectedVersion uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed.Load() {
		return s.errClosed()
	}
	if executionID == "" {
		return errors.New("store: execution ID cannot be empty")
	}
	if state == nil {
		return errors.New("store: state cannot be nil")
	}

	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("postgres: failed to marshal state json: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return s.wrapErr(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// First attempt atomic UPDATE where version = expectedVersion
	tag, err := tx.Exec(ctx, `
UPDATE executions
SET graph_id = $2, status = $3, version = $4, state_json = $5, updated_at = NOW()
WHERE execution_id = $1 AND version = $6
`, string(executionID), state.GraphID(), string(state.Status()), state.Version(), stateJSON, expectedVersion)
	if err != nil {
		return s.wrapErr(err)
	}

	if tag.RowsAffected() == 0 {
		// Either the row does not exist, or the stored version != expectedVersion
		var actualVersion uint64
		err := tx.QueryRow(ctx, "SELECT version FROM executions WHERE execution_id = $1", string(executionID)).Scan(&actualVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			if expectedVersion == 0 {
				// Initial state creation
				insertTag, err := tx.Exec(ctx, `
INSERT INTO executions (execution_id, graph_id, status, version, state_json, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
ON CONFLICT (execution_id) DO NOTHING
`, string(executionID), state.GraphID(), string(state.Status()), state.Version(), stateJSON)
				if err != nil {
					return s.wrapErr(err)
				}
				if insertTag.RowsAffected() == 0 {
					// Concurrent insert won the race
					_ = tx.QueryRow(ctx, "SELECT version FROM executions WHERE execution_id = $1", string(executionID)).Scan(&actualVersion)
					return &engine.StoreConflictError{
						ExecutionID:     executionID,
						ExpectedVersion: expectedVersion,
						ActualVersion:   actualVersion,
						Err:             engine.ErrStoreConflict,
					}
				}
			} else {
				return &engine.StoreConflictError{
					ExecutionID:     executionID,
					ExpectedVersion: expectedVersion,
					ActualVersion:   0,
					Err:             engine.ErrStoreConflict,
				}
			}
		} else if err != nil {
			return s.wrapErr(err)
		} else {
			// Row exists but version mismatched
			return &engine.StoreConflictError{
				ExecutionID:     executionID,
				ExpectedVersion: expectedVersion,
				ActualVersion:   actualVersion,
				Err:             engine.ErrStoreConflict,
			}
		}
	}

	// Upsert node states in node_states table
	for _, ns := range state.NodeStates() {
		outJSON, err := json.Marshal(ns.Output())
		if err != nil {
			return fmt.Errorf("postgres: marshal node output: %w", err)
		}
		_, err = tx.Exec(ctx, `
INSERT INTO node_states (execution_id, node_id, status, version, output_json, error_message, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, NOW())
ON CONFLICT (execution_id, node_id) DO UPDATE SET
    status = EXCLUDED.status,
    version = EXCLUDED.version,
    output_json = EXCLUDED.output_json,
    error_message = EXCLUDED.error_message,
    updated_at = NOW()
`, string(executionID), string(ns.NodeID()), string(ns.Status()), ns.Version(), outJSON, ns.ErrorMessage())
		if err != nil {
			return s.wrapErr(err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return s.wrapErr(err)
	}
	return nil
}

// PatchState atomically applies a StateMutation to the GraphState for executionID with optimistic concurrency.
func (s *Store) PatchState(ctx context.Context, executionID engine.ExecutionID, mutation *engine.StateMutation, expectedVersion uint64) (*engine.GraphState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.closed.Load() {
		return nil, s.errClosed()
	}
	if executionID == "" {
		return nil, errors.New("store: execution ID cannot be empty")
	}
	if mutation == nil {
		return nil, errors.New("store: mutation cannot be nil")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, s.wrapErr(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		stateJSON     []byte
		actualVersion uint64
	)
	err = tx.QueryRow(ctx, "SELECT state_json, version FROM executions WHERE execution_id = $1 FOR UPDATE", string(executionID)).Scan(&stateJSON, &actualVersion)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &engine.StoreNotFoundError{
				ExecutionID: executionID,
				Entity:      "GraphState",
				Err:         engine.ErrStoreNotFound,
			}
		}
		return nil, s.wrapErr(err)
	}

	if actualVersion != expectedVersion {
		return nil, &engine.StoreConflictError{
			ExecutionID:     executionID,
			ExpectedVersion: expectedVersion,
			ActualVersion:   actualVersion,
			Err:             engine.ErrStoreConflict,
		}
	}

	var currentState engine.GraphState
	if err := json.Unmarshal(stateJSON, &currentState); err != nil {
		return nil, fmt.Errorf("postgres: decode state json: %w", err)
	}

	newState, err := currentState.Apply(*mutation)
	if err != nil {
		return nil, err
	}

	newStateJSON, err := json.Marshal(newState)
	if err != nil {
		return nil, fmt.Errorf("postgres: marshal updated state: %w", err)
	}

	_, err = tx.Exec(ctx, `
UPDATE executions
SET graph_id = $2, status = $3, version = $4, state_json = $5, updated_at = NOW()
WHERE execution_id = $1
`, string(executionID), newState.GraphID(), string(newState.Status()), newState.Version(), newStateJSON)
	if err != nil {
		return nil, s.wrapErr(err)
	}

	// Update node_states for the mutated node
	mutatedNode := newState.NodeState(mutation.NodeID)
	if mutatedNode != nil {
		outJSON, err := json.Marshal(mutatedNode.Output())
		if err != nil {
			return nil, fmt.Errorf("postgres: marshal node output: %w", err)
		}
		_, err = tx.Exec(ctx, `
INSERT INTO node_states (execution_id, node_id, status, version, output_json, error_message, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, NOW())
ON CONFLICT (execution_id, node_id) DO UPDATE SET
    status = EXCLUDED.status,
    version = EXCLUDED.version,
    output_json = EXCLUDED.output_json,
    error_message = EXCLUDED.error_message,
    updated_at = NOW()
`, string(executionID), string(mutatedNode.NodeID()), string(mutatedNode.Status()), mutatedNode.Version(), outJSON, mutatedNode.ErrorMessage())
		if err != nil {
			return nil, s.wrapErr(err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, s.wrapErr(err)
	}

	return newState, nil
}

// AcquireLock attempts to acquire a distributed lock on resource with a lease TTL.
func (s *Store) AcquireLock(ctx context.Context, resource string, ttl time.Duration) (*engine.LockHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.closed.Load() {
		return nil, s.errClosed()
	}
	if resource == "" {
		return nil, errors.New("store: resource cannot be empty")
	}
	if ttl <= 0 {
		return nil, errors.New("store: ttl must be greater than zero")
	}

	token := generateLockToken()
	now := time.Now()
	expiresAt := now.Add(ttl)

	// Atomic insert or update if expired
	query := `
INSERT INTO distributed_locks (resource, token, expires_at)
VALUES ($1, $2, $3)
ON CONFLICT (resource) DO UPDATE
SET token = EXCLUDED.token, expires_at = EXCLUDED.expires_at
WHERE distributed_locks.expires_at < NOW()
RETURNING token
`
	var acquiredToken string
	err := s.pool.QueryRow(ctx, query, resource, token, expiresAt).Scan(&acquiredToken)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("resource %q is already locked", resource)
		}
		return nil, s.wrapErr(err)
	}

	return &engine.LockHandle{
		Resource:      resource,
		Token:         acquiredToken,
		LeaseDuration: ttl,
		AcquiredAt:    now,
	}, nil
}

// ReleaseLock safely releases a distributed lock if the token matches and has not expired.
func (s *Store) ReleaseLock(ctx context.Context, handle *engine.LockHandle) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed.Load() {
		return s.errClosed()
	}
	if handle == nil {
		return errors.New("store: lock handle cannot be nil")
	}
	if handle.Resource == "" {
		return errors.New("store: resource cannot be empty")
	}

	// Try atomic delete if token matches and not expired
	tag, err := s.pool.Exec(ctx, `
DELETE FROM distributed_locks
WHERE resource = $1 AND token = $2 AND expires_at > NOW()
`, handle.Resource, handle.Token)
	if err != nil {
		return s.wrapErr(err)
	}

	if tag.RowsAffected() == 0 {
		// Diagnose reason
		var (
			currentToken string
			expiresAt    time.Time
		)
		err := s.pool.QueryRow(ctx, "SELECT token, expires_at FROM distributed_locks WHERE resource = $1", handle.Resource).Scan(&currentToken, &expiresAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("store: lock handle is invalid or not owner")
		} else if err != nil {
			return s.wrapErr(err)
		}

		if currentToken != handle.Token {
			return errors.New("store: lock handle is invalid or not owner")
		}
		if !time.Now().Before(expiresAt) {
			// Clean up expired row
			_, _ = s.pool.Exec(ctx, "DELETE FROM distributed_locks WHERE resource = $1 AND token = $2", handle.Resource, handle.Token)
			return errors.New("store: lock handle has expired")
		}
		return errors.New("store: failed to release lock")
	}

	return nil
}

// ExtendLock extends the lease duration of a currently held distributed lock.
func (s *Store) ExtendLock(ctx context.Context, handle *engine.LockHandle, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed.Load() {
		return s.errClosed()
	}
	if handle == nil {
		return errors.New("store: lock handle cannot be nil")
	}
	if handle.Resource == "" {
		return errors.New("store: resource cannot be empty")
	}
	if ttl <= 0 {
		return errors.New("store: ttl must be greater than zero")
	}

	millis := ttl.Milliseconds()
	tag, err := s.pool.Exec(ctx, `
UPDATE distributed_locks
SET expires_at = expires_at + ($3 * INTERVAL '1 millisecond')
WHERE resource = $1 AND token = $2 AND expires_at > NOW()
`, handle.Resource, handle.Token, millis)
	if err != nil {
		return s.wrapErr(err)
	}

	if tag.RowsAffected() == 0 {
		var (
			currentToken string
			expiresAt    time.Time
		)
		err := s.pool.QueryRow(ctx, "SELECT token, expires_at FROM distributed_locks WHERE resource = $1", handle.Resource).Scan(&currentToken, &expiresAt)
		if errors.Is(err, pgx.ErrNoRows) || currentToken != handle.Token {
			return errors.New("store: lock handle is invalid or not owner")
		} else if err != nil {
			return s.wrapErr(err)
		}

		if !time.Now().Before(expiresAt) {
			return errors.New("store: lock has expired and cannot be extended")
		}
		return errors.New("store: failed to extend lock")
	}

	handle.LeaseDuration += ttl
	return nil
}

// AppendLog appends an audit log entry for the specified execution.
func (s *Store) AppendLog(ctx context.Context, entry *engine.ExecutionLogEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed.Load() {
		return s.errClosed()
	}
	if entry == nil {
		return errors.New("store: log entry cannot be nil")
	}
	if entry.ExecutionID == "" {
		return errors.New("store: execution ID cannot be empty")
	}

	detailsJSON := []byte("{}")
	if len(entry.Details) > 0 {
		var err error
		detailsJSON, err = json.Marshal(entry.Details)
		if err != nil {
			return fmt.Errorf("postgres: marshal log details: %w", err)
		}
	}

	_, err := s.pool.Exec(ctx, `
INSERT INTO execution_logs (execution_id, sequence, timestamp, message, level, details_json)
VALUES ($1, $2, $3, $4, 'INFO', $5)
`, string(entry.ExecutionID), entry.Sequence, entry.Timestamp, entry.Message, detailsJSON)
	if err != nil {
		return s.wrapErr(err)
	}
	return nil
}

// GetLogs queries audit log entries for an execution starting from fromSequence up to limit entries.
func (s *Store) GetLogs(ctx context.Context, executionID engine.ExecutionID, fromSequence uint64, limit int) ([]*engine.ExecutionLogEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.closed.Load() {
		return nil, s.errClosed()
	}
	if executionID == "" {
		return nil, errors.New("store: execution ID cannot be empty")
	}

	query := `
SELECT sequence, timestamp, message, details_json
FROM execution_logs
WHERE execution_id = $1 AND sequence >= $2
ORDER BY sequence ASC
`
	var (
		rows pgx.Rows
		err  error
	)
	if limit > 0 {
		query += " LIMIT $3"
		rows, err = s.pool.Query(ctx, query, string(executionID), fromSequence, limit)
	} else {
		rows, err = s.pool.Query(ctx, query, string(executionID), fromSequence)
	}
	if err != nil {
		return nil, s.wrapErr(err)
	}
	defer rows.Close()

	entries := make([]*engine.ExecutionLogEntry, 0)
	for rows.Next() {
		var (
			seq         uint64
			ts          time.Time
			msg         string
			detailsJSON []byte
		)
		if err := rows.Scan(&seq, &ts, &msg, &detailsJSON); err != nil {
			return nil, s.wrapErr(err)
		}

		var details map[string]any
		if len(detailsJSON) > 0 {
			_ = json.Unmarshal(detailsJSON, &details)
		}

		entries = append(entries, &engine.ExecutionLogEntry{
			ExecutionID: executionID,
			Sequence:    seq,
			Timestamp:   ts,
			Message:     msg,
			Details:     details,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, s.wrapErr(err)
	}

	return entries, nil
}

// PublishEvent publishes a single reactive engine event.
func (s *Store) PublishEvent(ctx context.Context, event *engine.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed.Load() {
		return s.errClosed()
	}
	if event == nil {
		return errors.New("store: event cannot be nil")
	}

	payloadJSON, err := json.Marshal(event.Payload())
	if err != nil {
		return fmt.Errorf("postgres: marshal event payload: %w", err)
	}

	_, err = s.pool.Exec(ctx, `
INSERT INTO execution_events (execution_id, sequence, timestamp, event_type, node_id, payload_json)
VALUES ($1, $2, $3, $4, $5, $6)
`, string(event.ExecutionID()), event.Sequence(), event.Timestamp(), string(event.Type()), string(event.NodeID()), payloadJSON)
	if err != nil {
		return s.wrapErr(err)
	}
	return nil
}

// PublishBatch publishes multiple reactive engine events in order.
func (s *Store) PublishBatch(ctx context.Context, events []*engine.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed.Load() {
		return s.errClosed()
	}
	if len(events) == 0 {
		return nil
	}
	for i, e := range events {
		if e == nil {
			return fmt.Errorf("store: batch event at index %d cannot be nil", i)
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return s.wrapErr(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for _, event := range events {
		payloadJSON, err := json.Marshal(event.Payload())
		if err != nil {
			return fmt.Errorf("postgres: marshal batch event payload: %w", err)
		}
		_, err = tx.Exec(ctx, `
INSERT INTO execution_events (execution_id, sequence, timestamp, event_type, node_id, payload_json)
VALUES ($1, $2, $3, $4, $5, $6)
`, string(event.ExecutionID()), event.Sequence(), event.Timestamp(), string(event.Type()), string(event.NodeID()), payloadJSON)
		if err != nil {
			return s.wrapErr(err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return s.wrapErr(err)
	}
	return nil
}

// QueryExecutions filters and queries executions by status, time range, and graph ID.
func (s *Store) QueryExecutions(ctx context.Context, filter ExecutionFilter) ([]*ExecutionSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.closed.Load() {
		return nil, s.errClosed()
	}

	query := `
SELECT execution_id, graph_id, status, version, created_at, updated_at
FROM executions
WHERE 1=1
`
	var args []any
	argIdx := 1

	if filter.GraphID != "" {
		query += fmt.Sprintf(" AND graph_id = $%d", argIdx)
		args = append(args, filter.GraphID)
		argIdx++
	}
	if filter.Status != "" {
		query += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, string(filter.Status))
		argIdx++
	}
	if filter.StartTime != nil {
		query += fmt.Sprintf(" AND created_at >= $%d", argIdx)
		args = append(args, *filter.StartTime)
		argIdx++
	}
	if filter.EndTime != nil {
		query += fmt.Sprintf(" AND created_at <= $%d", argIdx)
		args = append(args, *filter.EndTime)
		argIdx++
	}

	query += " ORDER BY created_at DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIdx)
		args = append(args, filter.Limit)
		argIdx++
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argIdx)
		args = append(args, filter.Offset)
		argIdx++
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, s.wrapErr(err)
	}
	defer rows.Close()

	summaries := make([]*ExecutionSummary, 0)
	for rows.Next() {
		var (
			execID    string
			graphID   string
			status    string
			version   uint64
			createdAt time.Time
			updatedAt time.Time
		)
		if err := rows.Scan(&execID, &graphID, &status, &version, &createdAt, &updatedAt); err != nil {
			return nil, s.wrapErr(err)
		}
		summaries = append(summaries, &ExecutionSummary{
			ExecutionID: engine.ExecutionID(execID),
			GraphID:     graphID,
			Status:      engine.Status(status),
			Version:     version,
			CreatedAt:   createdAt,
			UpdatedAt:   updatedAt,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, s.wrapErr(err)
	}
	return summaries, nil
}

// QueryNodeStates queries individual node states by executionID, nodeID, and status.
func (s *Store) QueryNodeStates(ctx context.Context, executionID engine.ExecutionID, nodeID engine.NodeID, status engine.Status) ([]*NodeStateRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.closed.Load() {
		return nil, s.errClosed()
	}

	query := `
SELECT execution_id, node_id, status, version, output_json, error_message, updated_at
FROM node_states
WHERE 1=1
`
	var args []any
	argIdx := 1

	if executionID != "" {
		query += fmt.Sprintf(" AND execution_id = $%d", argIdx)
		args = append(args, string(executionID))
		argIdx++
	}
	if nodeID != "" {
		query += fmt.Sprintf(" AND node_id = $%d", argIdx)
		args = append(args, string(nodeID))
		argIdx++
	}
	if status != "" {
		query += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, string(status))
		argIdx++
	}

	query += " ORDER BY execution_id, node_id"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, s.wrapErr(err)
	}
	defer rows.Close()

	records := make([]*NodeStateRecord, 0)
	for rows.Next() {
		var (
			execID    string
			nID       string
			st        string
			version   uint64
			outJSON   []byte
			errMsg    string
			updatedAt time.Time
		)
		if err := rows.Scan(&execID, &nID, &st, &version, &outJSON, &errMsg, &updatedAt); err != nil {
			return nil, s.wrapErr(err)
		}

		var output map[string]any
		if len(outJSON) > 0 {
			_ = json.Unmarshal(outJSON, &output)
		}

		records = append(records, &NodeStateRecord{
			ExecutionID:  engine.ExecutionID(execID),
			NodeID:       engine.NodeID(nID),
			Status:       engine.Status(st),
			Version:      version,
			Output:       output,
			ErrorMessage: errMsg,
			UpdatedAt:    updatedAt,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, s.wrapErr(err)
	}
	return records, nil
}

// QueryAuditTrail queries audit logs for an execution within an optional time range.
func (s *Store) QueryAuditTrail(ctx context.Context, executionID engine.ExecutionID, fromTime *time.Time, toTime *time.Time) ([]*engine.ExecutionLogEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.closed.Load() {
		return nil, s.errClosed()
	}
	if executionID == "" {
		return nil, errors.New("store: execution ID cannot be empty")
	}

	query := `
SELECT sequence, timestamp, message, details_json
FROM execution_logs
WHERE execution_id = $1
`
	args := []any{string(executionID)}
	argIdx := 2

	if fromTime != nil {
		query += fmt.Sprintf(" AND timestamp >= $%d", argIdx)
		args = append(args, *fromTime)
		argIdx++
	}
	if toTime != nil {
		query += fmt.Sprintf(" AND timestamp <= $%d", argIdx)
		args = append(args, *toTime)
		argIdx++
	}

	query += " ORDER BY sequence ASC"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, s.wrapErr(err)
	}
	defer rows.Close()

	entries := make([]*engine.ExecutionLogEntry, 0)
	for rows.Next() {
		var (
			seq         uint64
			ts          time.Time
			msg         string
			detailsJSON []byte
		)
		if err := rows.Scan(&seq, &ts, &msg, &detailsJSON); err != nil {
			return nil, s.wrapErr(err)
		}

		var details map[string]any
		if len(detailsJSON) > 0 {
			_ = json.Unmarshal(detailsJSON, &details)
		}

		entries = append(entries, &engine.ExecutionLogEntry{
			ExecutionID: executionID,
			Sequence:    seq,
			Timestamp:   ts,
			Message:     msg,
			Details:     details,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, s.wrapErr(err)
	}
	return entries, nil
}

func (s *Store) errClosed() error {
	return &engine.StoreUnavailableError{
		Backend: "postgres",
		Err:     errors.New("store is closed"),
	}
}

func (s *Store) wrapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if engine.IsStoreNotFound(err) || engine.IsStoreConflict(err) || engine.IsStoreUnavailable(err) {
		return err
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return wrapUnavailable(err)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// Connection exceptions (08xxx) or operator intervention (57xxx)
		if strings.HasPrefix(pgErr.Code, "08") || strings.HasPrefix(pgErr.Code, "57") {
			return wrapUnavailable(err)
		}
	}
	return err
}

func wrapUnavailable(err error) error {
	return &engine.StoreUnavailableError{
		Backend: "postgres",
		Err:     err,
	}
}

func generateLockToken() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
