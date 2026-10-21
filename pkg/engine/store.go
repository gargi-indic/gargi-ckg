package engine

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors for StateStore and capability operations.
// These sentinel errors can be matched using errors.Is.
var (
	// ErrStoreNotFound is returned when an execution state, lock, or entity is not found in the store.
	ErrStoreNotFound = errors.New("store: entity not found")

	// ErrStoreConflict is returned when an optimistic concurrency check fails or a state conflict occurs.
	ErrStoreConflict = errors.New("store: optimistic concurrency conflict")

	// ErrStoreUnavailable is returned when the underlying storage backend is temporarily unreachable or unavailable.
	ErrStoreUnavailable = errors.New("store: storage backend unavailable")
)

// StoreNotFoundError indicates that a requested entity could not be found in the store.
type StoreNotFoundError struct {
	ExecutionID ExecutionID
	Entity      string
	Err         error
}

// Error returns the formatted error string for StoreNotFoundError.
func (e *StoreNotFoundError) Error() string {
	if e == nil {
		return "store: entity not found"
	}
	msg := "store: not found"
	if e.Entity != "" {
		msg = fmt.Sprintf("store: %s not found", e.Entity)
	}
	if e.ExecutionID != "" {
		msg = fmt.Sprintf("%s (executionID: %s)", msg, e.ExecutionID)
	}
	if e.Err != nil {
		msg = fmt.Sprintf("%s: %v", msg, e.Err)
	}
	return msg
}

// Unwrap returns the underlying error or ErrStoreNotFound for error unwrapping.
func (e *StoreNotFoundError) Unwrap() error {
	if e == nil {
		return nil
	}
	if e.Err != nil {
		return e.Err
	}
	return ErrStoreNotFound
}

// Is reports whether this error matches target.
func (e *StoreNotFoundError) Is(target error) bool {
	return target == ErrStoreNotFound
}

// StoreConflictError indicates that an optimistic concurrency check failed due to a version mismatch.
type StoreConflictError struct {
	ExecutionID     ExecutionID
	ExpectedVersion uint64
	ActualVersion   uint64
	Err             error
}

// Error returns the formatted error string for StoreConflictError.
func (e *StoreConflictError) Error() string {
	if e == nil {
		return "store: optimistic concurrency conflict"
	}
	msg := fmt.Sprintf("store: conflict (expected version %d, actual version %d)", e.ExpectedVersion, e.ActualVersion)
	if e.ExecutionID != "" {
		msg = fmt.Sprintf("%s for executionID %s", msg, e.ExecutionID)
	}
	if e.Err != nil {
		msg = fmt.Sprintf("%s: %v", msg, e.Err)
	}
	return msg
}

// Unwrap returns the underlying error or ErrStoreConflict for error unwrapping.
func (e *StoreConflictError) Unwrap() error {
	if e == nil {
		return nil
	}
	if e.Err != nil {
		return e.Err
	}
	return ErrStoreConflict
}

// Is reports whether this error matches target.
func (e *StoreConflictError) Is(target error) bool {
	return target == ErrStoreConflict
}

// StoreUnavailableError indicates that the storage backend is temporarily unreachable or unavailable.
type StoreUnavailableError struct {
	Backend string
	Err     error
}

// Error returns the formatted error string for StoreUnavailableError.
func (e *StoreUnavailableError) Error() string {
	if e == nil {
		return "store: storage backend unavailable"
	}
	msg := "store: storage backend unavailable"
	if e.Backend != "" {
		msg = fmt.Sprintf("store: %s backend unavailable", e.Backend)
	}
	if e.Err != nil {
		msg = fmt.Sprintf("%s: %v", msg, e.Err)
	}
	return msg
}

// Unwrap returns the underlying error or ErrStoreUnavailable for error unwrapping.
func (e *StoreUnavailableError) Unwrap() error {
	if e == nil {
		return nil
	}
	if e.Err != nil {
		return e.Err
	}
	return ErrStoreUnavailable
}

// Is reports whether this error matches target.
func (e *StoreUnavailableError) Is(target error) bool {
	return target == ErrStoreUnavailable
}

// IsStoreNotFound reports whether err represents an entity not found error.
func IsStoreNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrStoreNotFound) {
		return true
	}
	var notFoundErr *StoreNotFoundError
	return errors.As(err, &notFoundErr)
}

// IsStoreConflict reports whether err represents an optimistic concurrency conflict error.
func IsStoreConflict(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrStoreConflict) {
		return true
	}
	var conflictErr *StoreConflictError
	return errors.As(err, &conflictErr)
}

// IsStoreUnavailable reports whether err represents a storage unavailable error.
func IsStoreUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrStoreUnavailable) {
		return true
	}
	var unavailErr *StoreUnavailableError
	return errors.As(err, &unavailErr)
}

// StateStore defines the public contract for execution state persistence.
// Implementations must support atomic Compare-And-Swap (CAS) optimistic concurrency
// via expectedVersion matching GraphState.Version().
type StateStore interface {
	// GetState retrieves the current GraphState for the given executionID.
	// Returns a StoreNotFoundError (or ErrStoreNotFound) if no state exists for the executionID.
	GetState(ctx context.Context, executionID ExecutionID) (*GraphState, error)

	// SetState stores or updates the entire GraphState for an execution.
	// It enforces optimistic concurrency: expectedVersion must match the current version
	// stored for executionID. If creating initial state, expectedVersion should match the initial state's expected version (typically 0).
	// If expectedVersion does not match the actual stored version, a StoreConflictError must be returned.
	// On success, the state is persisted.
	SetState(ctx context.Context, executionID ExecutionID, state *GraphState, expectedVersion uint64) error

	// PatchState atomically applies a StateMutation to the GraphState for executionID.
	// It enforces optimistic concurrency against expectedVersion.
	// Returns the updated GraphState on success, or a StoreConflictError if expectedVersion does not match.
	// If executionID is not found, returns StoreNotFoundError.
	PatchState(ctx context.Context, executionID ExecutionID, mutation *StateMutation, expectedVersion uint64) (*GraphState, error)

	// ListExecutions retrieves a list of execution summaries matching the given filter.
	ListExecutions(ctx context.Context, filter ExecutionFilter) ([]*ExecutionSummary, error)
}

// LockHandle represents an acquired distributed lock token.
type LockHandle struct {
	// Resource is the unique identifier for the locked resource.
	Resource string
	// Token is the unique ownership token (e.g. UUID/fencing token) of the lock holder.
	Token string
	// LeaseDuration is the validity duration granted when the lock was acquired or extended.
	LeaseDuration time.Duration
	// AcquiredAt is the timestamp when the lock was granted.
	AcquiredAt time.Time
}

// LockManager defines capability for distributed mutual exclusion and locking.
type LockManager interface {
	// AcquireLock attempts to acquire a distributed lock for the given resource with a lease duration.
	// Returns a LockHandle on success, or an error if the lock cannot be acquired or context is cancelled.
	AcquireLock(ctx context.Context, resource string, ttl time.Duration) (*LockHandle, error)

	// ReleaseLock releases the lock associated with the given handle.
	// Returns an error if the lock handle has expired or is invalid.
	ReleaseLock(ctx context.Context, handle *LockHandle) error

	// ExtendLock extends the lease duration of an existing lock handle.
	// Returns an error if the lock was lost or cannot be extended.
	ExtendLock(ctx context.Context, handle *LockHandle, ttl time.Duration) error
}

// EventPublisher defines capability for publishing reactive execution events.
type EventPublisher interface {
	// PublishEvent publishes a single reactive engine event to downstream subscribers.
	PublishEvent(ctx context.Context, event *Event) error

	// PublishBatch publishes multiple reactive engine events in order.
	PublishBatch(ctx context.Context, events []*Event) error
}

// ExecutionLogEntry represents an individual append-only audit log entry for an execution.
type ExecutionLogEntry struct {
	// ExecutionID is the unique identifier of the graph execution.
	ExecutionID ExecutionID `json:"executionId"`
	// Sequence is the monotonically increasing sequence number within the execution log.
	Sequence uint64 `json:"sequence"`
	// Timestamp is the time the log entry was recorded.
	Timestamp time.Time `json:"timestamp"`
	// Message is the log message or action description.
	Message string `json:"message"`
	// Details contains arbitrary structured metadata for auditing.
	Details map[string]any `json:"details,omitempty"`
}

// ExecutionLog defines capability for append-only audit logging and query.
type ExecutionLog interface {
	// AppendLog appends an audit log entry for the specified execution.
	AppendLog(ctx context.Context, entry *ExecutionLogEntry) error

	// GetLogs queries audit log entries for an execution starting from fromSequence up to limit entries.
	// If limit <= 0, a default system limit or all entries may be returned.
	GetLogs(ctx context.Context, executionID ExecutionID, fromSequence uint64, limit int) ([]*ExecutionLogEntry, error)
}

// ExecutionFilter defines filtering parameters for ListExecutions.
type ExecutionFilter struct {
	GraphID   string
	Status    Status
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Offset    int
}

// ExecutionSummary represents summary metadata for an execution in ListExecutions.
type ExecutionSummary struct {
	ExecutionID ExecutionID `json:"executionId"`
	GraphID     string      `json:"graphId"`
	Status      Status      `json:"status"`
	Version     uint64      `json:"version"`
	CreatedAt   time.Time   `json:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt"`
}
