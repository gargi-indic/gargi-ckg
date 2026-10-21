package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
	redis "github.com/redis/go-redis/v9"
)

var (
	_ engine.StateStore     = (*Store)(nil)
	_ engine.LockManager    = (*Store)(nil)
	_ engine.EventPublisher = (*Store)(nil)
	_ engine.ExecutionLog   = (*Store)(nil)
)

// Default configurations
const (
	DefaultKeyPrefix              = "gargi"
	DefaultSubscriptionBufferSize = 64
	DefaultMaxPatchRetries        = 5
	DefaultExecutionLogLimit      = 1000
)

// Lua scripts
var (
	// setStateScript atomically checks version before storing new state.
	// KEYS[1]: state key
	// ARGV[1]: expectedVersion (string integer)
	// ARGV[2]: newStateJSON
	// Returns:
	//   1 -> success
	//   {0, actualVersion} -> version conflict (or {0, -1} if key did not exist and expectedVersion != 0)
	setStateScript = redis.NewScript(`
local current = redis.call("get", KEYS[1])
local expected = tonumber(ARGV[1])
if not current then
    if expected == 0 then
        redis.call("set", KEYS[1], ARGV[2])
        return 1
    else
        return {0, -1}
    end
end
local decoded = cjson.decode(current)
local actual = tonumber(decoded["version"])
if not actual then
    actual = 0
end
if actual == expected then
    redis.call("set", KEYS[1], ARGV[2])
    return 1
else
    return {0, actual}
end
`)

	// patchStateScript atomically checks version, applies new state JSON, and returns 1 on success or actual version on mismatch.
	// KEYS[1]: state key
	// ARGV[1]: expectedVersion (string integer)
	// ARGV[2]: newStateJSON
	// Returns:
	//   1 -> success
	//   -1 -> key not found
	//   {0, actualVersion} -> conflict
	patchStateScript = redis.NewScript(`
local current = redis.call("get", KEYS[1])
if not current then
    return -1
end
local expected = tonumber(ARGV[1])
local decoded = cjson.decode(current)
local actual = tonumber(decoded["version"])
if not actual then
    actual = 0
end
if actual == expected then
    redis.call("set", KEYS[1], ARGV[2])
    return 1
else
    return {0, actual}
end
`)

	// releaseLockScript releases a lock only if the token matches.
	// KEYS[1]: lock key
	// ARGV[1]: lock token
	// Returns:
	//   1 -> lock released
	//   0 -> token mismatch or lock expired/missing
	releaseLockScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
    return redis.call("del", KEYS[1])
else
    return 0
end
`)

	// extendLockScript extends the TTL only if the token matches.
	// KEYS[1]: lock key
	// ARGV[1]: lock token
	// ARGV[2]: ttl milliseconds
	// Returns:
	//   1 -> ttl extended
	//   0 -> token mismatch or lock expired/missing
	extendLockScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
    return redis.call("pexpire", KEYS[1], ARGV[2])
else
    return 0
end
`)
)

// Config holds configuration parameters for connecting to Redis.
type Config struct {
	// Addr is the Redis address (host:port).
	Addr string
	// Username for Redis ACL authentication.
	Username string
	// Password for Redis authentication.
	Password string
	// DB is the Redis database index (default 0).
	DB int
	// KeyPrefix is the namespace prefix applied to all Redis keys. Defaults to "gargi".
	KeyPrefix string
	// Client is an optional pre-configured redis.UniversalClient. If provided, Addr/Username/Password/DB are ignored.
	Client redis.UniversalClient
	// SubscriptionBufferSize is the buffer capacity for event subscriber channels. Defaults to 64.
	SubscriptionBufferSize int
	// MaxPatchRetries is the maximum number of optimistic concurrency retries during PatchState. Defaults to 5.
	MaxPatchRetries int
}

// Option modifies a Config instance.
type Option func(*Config)

// WithKeyPrefix sets a custom key prefix.
func WithKeyPrefix(prefix string) Option {
	return func(c *Config) {
		c.KeyPrefix = prefix
	}
}

// WithSubscriptionBufferSize sets the event channel buffer size.
func WithSubscriptionBufferSize(size int) Option {
	return func(c *Config) {
		if size > 0 {
			c.SubscriptionBufferSize = size
		}
	}
}

// WithClient sets a custom Redis client.
func WithClient(client redis.UniversalClient) Option {
	return func(c *Config) {
		c.Client = client
	}
}

// Store is a Redis-backed implementation of engine.StateStore, engine.LockManager,
// engine.EventPublisher, and engine.ExecutionLog.
type Store struct {
	client        redis.UniversalClient
	ownsClient    bool
	keyPrefix     string
	subBufferSize int
	maxPatchRetry int
	closed        atomic.Bool

	subMu      sync.Mutex
	activeSubs []*subscriber
}

type subscriber struct {
	id     uint64
	cancel context.CancelFunc
}

var nextSubID atomic.Uint64

// New creates and initializes a new Redis Store adapter.
func New(cfg Config, opts ...Option) (*Store, error) {
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	if cfg.KeyPrefix == "" {
		cfg.KeyPrefix = DefaultKeyPrefix
	}
	cfg.KeyPrefix = strings.TrimSuffix(cfg.KeyPrefix, ":")

	if cfg.SubscriptionBufferSize <= 0 {
		cfg.SubscriptionBufferSize = DefaultSubscriptionBufferSize
	}
	if cfg.MaxPatchRetries <= 0 {
		cfg.MaxPatchRetries = DefaultMaxPatchRetries
	}

	var client redis.UniversalClient
	ownsClient := false

	if cfg.Client != nil {
		client = cfg.Client
	} else {
		if cfg.Addr == "" {
			cfg.Addr = "localhost:6379"
		}
		client = redis.NewClient(&redis.Options{
			Addr:     cfg.Addr,
			Username: cfg.Username,
			Password: cfg.Password,
			DB:       cfg.DB,
		})
		ownsClient = true
	}

	return &Store{
		client:        client,
		ownsClient:    ownsClient,
		keyPrefix:     cfg.KeyPrefix,
		subBufferSize: cfg.SubscriptionBufferSize,
		maxPatchRetry: cfg.MaxPatchRetries,
	}, nil
}

// NewStore is an alias for New.
func NewStore(cfg Config, opts ...Option) (*Store, error) {
	return New(cfg, opts...)
}

// Client returns the underlying Redis client.
func (s *Store) Client() redis.UniversalClient {
	return s.client
}

func (s *Store) stateKey(executionID engine.ExecutionID) string {
	return fmt.Sprintf("%s:state:%s", s.keyPrefix, executionID)
}

func (s *Store) lockKey(resource string) string {
	return fmt.Sprintf("%s:lock:%s", s.keyPrefix, resource)
}

func (s *Store) eventsKey(executionID engine.ExecutionID) string {
	return fmt.Sprintf("%s:events:%s", s.keyPrefix, executionID)
}

func (s *Store) allEventsKey() string {
	return fmt.Sprintf("%s:events:all", s.keyPrefix)
}

func (s *Store) logsKey(executionID engine.ExecutionID) string {
	return fmt.Sprintf("%s:logs:%s", s.keyPrefix, executionID)
}

func (s *Store) checkClosed() error {
	if s.closed.Load() {
		return &engine.StoreUnavailableError{
			Backend: "redis",
			Err:     errors.New("store is closed"),
		}
	}
	return nil
}

func (s *Store) wrapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, redis.Nil) {
		return engine.ErrStoreNotFound
	}
	var netErr net.Error
	if errors.As(err, &netErr) || errors.Is(err, io.EOF) || strings.Contains(strings.ToLower(err.Error()), "connection") || strings.Contains(strings.ToLower(err.Error()), "closed") {
		return &engine.StoreUnavailableError{
			Backend: "redis",
			Err:     err,
		}
	}
	return err
}

// GetState retrieves the current GraphState for the given executionID.
func (s *Store) GetState(ctx context.Context, executionID engine.ExecutionID) (*engine.GraphState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if executionID == "" {
		return nil, errors.New("store: execution ID cannot be empty")
	}
	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	key := s.stateKey(executionID)
	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, &engine.StoreNotFoundError{
				ExecutionID: executionID,
				Entity:      "GraphState",
				Err:         engine.ErrStoreNotFound,
			}
		}
		return nil, s.wrapError(err)
	}

	var state engine.GraphState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("store: failed to unmarshal state: %w", err)
	}
	return &state, nil
}

// SetState stores or updates the entire GraphState for an execution enforcing CAS concurrency.
func (s *Store) SetState(ctx context.Context, executionID engine.ExecutionID, state *engine.GraphState, expectedVersion uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.checkClosed(); err != nil {
		return err
	}
	if executionID == "" {
		return errors.New("store: execution ID cannot be empty")
	}
	if state == nil {
		return errors.New("store: state cannot be nil")
	}

	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("store: failed to marshal state: %w", err)
	}

	key := s.stateKey(executionID)
	res, err := setStateScript.Run(ctx, s.client, []string{key}, expectedVersion, string(stateJSON)).Result()
	if err != nil {
		return s.wrapError(err)
	}

	switch val := res.(type) {
	case int64:
		if val == 1 {
			return nil
		}
	case []any:
		if len(val) >= 2 {
			actual := parseUint64(val[1])
			return &engine.StoreConflictError{
				ExecutionID:     executionID,
				ExpectedVersion: expectedVersion,
				ActualVersion:   actual,
				Err:             engine.ErrStoreConflict,
			}
		}
	}

	return nil
}

// PatchState atomically applies a StateMutation to the GraphState for executionID with CAS validation.
func (s *Store) PatchState(ctx context.Context, executionID engine.ExecutionID, mutation *engine.StateMutation, expectedVersion uint64) (*engine.GraphState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.checkClosed(); err != nil {
		return nil, err
	}
	if executionID == "" {
		return nil, errors.New("store: execution ID cannot be empty")
	}
	if mutation == nil {
		return nil, errors.New("store: mutation cannot be nil")
	}

	// Retrieve current state
	currentState, err := s.GetState(ctx, executionID)
	if err != nil {
		return nil, err
	}

	// Validate expected version against retrieved state
	if currentState.Version() != expectedVersion {
		return nil, &engine.StoreConflictError{
			ExecutionID:     executionID,
			ExpectedVersion: expectedVersion,
			ActualVersion:   currentState.Version(),
			Err:             engine.ErrStoreConflict,
		}
	}

	// Apply mutation locally
	newState, err := currentState.Apply(*mutation)
	if err != nil {
		return nil, err
	}

	newStateJSON, err := json.Marshal(newState)
	if err != nil {
		return nil, fmt.Errorf("store: failed to marshal mutated state: %w", err)
	}

	key := s.stateKey(executionID)
	res, err := patchStateScript.Run(ctx, s.client, []string{key}, expectedVersion, string(newStateJSON)).Result()
	if err != nil {
		return nil, s.wrapError(err)
	}

	switch val := res.(type) {
	case int64:
		if val == 1 {
			return newState, nil
		}
		if val == -1 {
			return nil, &engine.StoreNotFoundError{
				ExecutionID: executionID,
				Entity:      "GraphState",
				Err:         engine.ErrStoreNotFound,
			}
		}
	case []any:
		if len(val) >= 2 {
			actual := parseUint64(val[1])
			return nil, &engine.StoreConflictError{
				ExecutionID:     executionID,
				ExpectedVersion: expectedVersion,
				ActualVersion:   actual,
				Err:             engine.ErrStoreConflict,
			}
		}
	}

	return newState, nil
}

// AcquireLock attempts to acquire a distributed lock on the given resource with the provided TTL.
func (s *Store) AcquireLock(ctx context.Context, resource string, ttl time.Duration) (*engine.LockHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if resource == "" {
		return nil, errors.New("store: resource cannot be empty")
	}
	if ttl <= 0 {
		return nil, errors.New("store: ttl must be greater than zero")
	}
	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	token := generateLockToken()
	key := s.lockKey(resource)
	now := time.Now()

	ok, err := s.client.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return nil, s.wrapError(err)
	}
	if !ok {
		return nil, fmt.Errorf("store: resource %q is already locked", resource)
	}

	return &engine.LockHandle{
		Resource:      resource,
		Token:         token,
		LeaseDuration: ttl,
		AcquiredAt:    now,
	}, nil
}

// ReleaseLock releases the lock associated with the given handle.
func (s *Store) ReleaseLock(ctx context.Context, handle *engine.LockHandle) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if handle == nil {
		return errors.New("store: lock handle cannot be nil")
	}
	if handle.Resource == "" {
		return errors.New("store: resource cannot be empty")
	}
	if err := s.checkClosed(); err != nil {
		return err
	}

	key := s.lockKey(handle.Resource)
	res, err := releaseLockScript.Run(ctx, s.client, []string{key}, handle.Token).Int64()
	if err != nil {
		return s.wrapError(err)
	}
	if res == 0 {
		return errors.New("store: lock handle is invalid, expired, or not owned")
	}
	return nil
}

// ExtendLock extends the lease duration of an existing held lock handle.
func (s *Store) ExtendLock(ctx context.Context, handle *engine.LockHandle, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if handle == nil {
		return errors.New("store: lock handle cannot be nil")
	}
	if ttl <= 0 {
		return errors.New("store: ttl must be greater than zero")
	}
	if err := s.checkClosed(); err != nil {
		return err
	}

	key := s.lockKey(handle.Resource)
	res, err := extendLockScript.Run(ctx, s.client, []string{key}, handle.Token, ttl.Milliseconds()).Int64()
	if err != nil {
		return s.wrapError(err)
	}
	if res == 0 {
		return errors.New("store: lock has expired or token is invalid and cannot be extended")
	}

	handle.LeaseDuration += ttl
	return nil
}

// PublishEvent publishes a single reactive engine event to Redis pub/sub channels.
func (s *Store) PublishEvent(ctx context.Context, event *engine.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.checkClosed(); err != nil {
		return err
	}
	if event == nil {
		return errors.New("store: event cannot be nil")
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("store: failed to marshal event: %w", err)
	}

	// Publish to execution-specific channel and global channel
	targetKey := s.eventsKey(event.ExecutionID())
	allKey := s.allEventsKey()

	pipe := s.client.Pipeline()
	pipe.Publish(ctx, targetKey, string(data))
	pipe.Publish(ctx, allKey, string(data))
	_, err = pipe.Exec(ctx)
	if err != nil {
		return s.wrapError(err)
	}
	return nil
}

// PublishBatch publishes multiple reactive engine events in sequential order.
func (s *Store) PublishBatch(ctx context.Context, events []*engine.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.checkClosed(); err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}
	for i, e := range events {
		if e == nil {
			return fmt.Errorf("store: batch event at index %d cannot be nil", i)
		}
	}

	pipe := s.client.Pipeline()
	allKey := s.allEventsKey()

	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("store: failed to marshal event: %w", err)
		}
		pipe.Publish(ctx, s.eventsKey(event.ExecutionID()), string(data))
		pipe.Publish(ctx, allKey, string(data))
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return s.wrapError(err)
	}
	return nil
}

// Subscribe subscribes to events for a specific executionID via Redis PubSub.
// If executionID is empty, it subscribes to all events across all executions.
// When ctx is canceled or the store is closed, the subscription channel is closed.
func (s *Store) Subscribe(ctx context.Context, executionID engine.ExecutionID) (<-chan *engine.Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	var channelName string
	if executionID == "" {
		channelName = s.allEventsKey()
	} else {
		channelName = s.eventsKey(executionID)
	}

	pubsub := s.client.Subscribe(ctx, channelName)
	// Confirm subscription was received
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return nil, s.wrapError(err)
	}

	subCtx, cancel := context.WithCancel(ctx)
	ch := make(chan *engine.Event, s.subBufferSize)
	subID := nextSubID.Add(1)

	sub := &subscriber{
		id:     subID,
		cancel: cancel,
	}

	s.subMu.Lock()
	s.activeSubs = append(s.activeSubs, sub)
	s.subMu.Unlock()

	redisMsgCh := pubsub.Channel()

	go func() {
		defer func() {
			_ = pubsub.Close()
			close(ch)
			s.removeSub(subID)
		}()

		for {
			select {
			case <-subCtx.Done():
				return
			case msg, ok := <-redisMsgCh:
				if !ok {
					return
				}
				var event engine.Event
				if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
					continue
				}
				select {
				case ch <- &event:
				case <-subCtx.Done():
					return
				default:
					// If channel buffer is full, drop or let it proceed without blocking subscriber goroutine
				}
			}
		}
	}()

	return ch, nil
}

// SubscribeAll subscribes to events across all execution IDs.
func (s *Store) SubscribeAll(ctx context.Context) (<-chan *engine.Event, error) {
	return s.Subscribe(ctx, "")
}

func (s *Store) removeSub(id uint64) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for i, sub := range s.activeSubs {
		if sub.id == id {
			s.activeSubs = append(s.activeSubs[:i], s.activeSubs[i+1:]...)
			break
		}
	}
}

// AppendLog appends an audit log entry for the specified execution using RPUSH.
func (s *Store) AppendLog(ctx context.Context, entry *engine.ExecutionLogEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if entry == nil {
		return errors.New("store: log entry cannot be nil")
	}
	if entry.ExecutionID == "" {
		return errors.New("store: execution ID cannot be empty")
	}
	if err := s.checkClosed(); err != nil {
		return err
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("store: failed to marshal log entry: %w", err)
	}

	key := s.logsKey(entry.ExecutionID)
	if err := s.client.RPush(ctx, key, string(data)).Err(); err != nil {
		return s.wrapError(err)
	}
	return nil
}

// GetLogs queries audit log entries for an execution starting from fromSequence up to limit entries.
func (s *Store) GetLogs(ctx context.Context, executionID engine.ExecutionID, fromSequence uint64, limit int) ([]*engine.ExecutionLogEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if executionID == "" {
		return nil, errors.New("store: execution ID cannot be empty")
	}
	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	key := s.logsKey(executionID)
	// Fetch all log entries from the list (from start 0 to -1)
	items, err := s.client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, s.wrapError(err)
	}
	if len(items) == 0 {
		return []*engine.ExecutionLogEntry{}, nil
	}

	res := make([]*engine.ExecutionLogEntry, 0, len(items))
	for _, item := range items {
		var entry engine.ExecutionLogEntry
		if err := json.Unmarshal([]byte(item), &entry); err != nil {
			continue
		}
		if entry.Sequence >= fromSequence {
			res = append(res, &entry)
			if limit > 0 && len(res) >= limit {
				break
			}
		}
	}
	return res, nil
}

// Reset deletes all gargi keys created under this key prefix.
func (s *Store) Reset(ctx context.Context) error {
	if err := s.checkClosed(); err != nil {
		return err
	}
	var cursor uint64
	matchPattern := fmt.Sprintf("%s:*", s.keyPrefix)
	for {
		keys, nextCursor, err := s.client.Scan(ctx, cursor, matchPattern, 100).Result()
		if err != nil {
			return s.wrapError(err)
		}
		if len(keys) > 0 {
			if err := s.client.Del(ctx, keys...).Err(); err != nil {
				return s.wrapError(err)
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return nil
}

// Close closes the Redis store and cancels all active subscriptions.
// If the store created its own Redis client, it will also close the client connection.
func (s *Store) Close() error {
	if s.closed.Swap(true) {
		return nil
	}

	s.subMu.Lock()
	for _, sub := range s.activeSubs {
		sub.cancel()
	}
	s.activeSubs = nil
	s.subMu.Unlock()

	if s.ownsClient && s.client != nil {
		return s.client.Close()
	}
	return nil
}

var fallbackTokenSeq uint64

func generateLockToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("token-%d-%d", time.Now().UnixNano(), atomic.AddUint64(&fallbackTokenSeq, 1))
	}
	return hex.EncodeToString(b)
}

func parseUint64(v any) uint64 {
	switch val := v.(type) {
	case int64:
		if val < 0 {
			return 0
		}
		return uint64(val)
	case uint64:
		return val
	case float64:
		if val < 0 {
			return 0
		}
		return uint64(val)
	case string:
		u, _ := strconv.ParseUint(val, 10, 64)
		return u
	default:
		return 0
	}
}

// ListExecutions retrieves a list of execution summaries matching the given filter.
func (s *Store) ListExecutions(ctx context.Context, filter engine.ExecutionFilter) ([]*engine.ExecutionSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	pattern := fmt.Sprintf("%s:state:*", s.keyPrefix)
	keys, err := s.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, s.wrapError(err)
	}

	var summaries []*engine.ExecutionSummary
	for _, k := range keys {
		data, err := s.client.Get(ctx, k).Bytes()
		if err != nil {
			continue
		}
		var state engine.GraphState
		if err := json.Unmarshal(data, &state); err != nil {
			continue
		}

		if filter.GraphID != "" && state.GraphID() != filter.GraphID {
			continue
		}
		if filter.Status != "" && state.Status() != filter.Status {
			continue
		}

		summaries = append(summaries, &engine.ExecutionSummary{
			ExecutionID: state.ExecutionID(),
			GraphID:     state.GraphID(),
			Status:      state.Status(),
			Version:     state.Version(),
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].ExecutionID > summaries[j].ExecutionID
	})

	if filter.Offset > 0 {
		if filter.Offset >= len(summaries) {
			return []*engine.ExecutionSummary{}, nil
		}
		summaries = summaries[filter.Offset:]
	}

	if filter.Limit > 0 && len(summaries) > filter.Limit {
		summaries = summaries[:filter.Limit]
	}

	return summaries, nil
}
