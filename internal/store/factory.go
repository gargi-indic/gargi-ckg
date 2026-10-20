package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/gargi-indic/gargi-flow/internal/config"
	"github.com/gargi-indic/gargi-flow/internal/store/memory"
	"github.com/gargi-indic/gargi-flow/internal/store/neo4j"
	"github.com/gargi-indic/gargi-flow/internal/store/postgres"
	"github.com/gargi-indic/gargi-flow/internal/store/redis"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
)

// Builder facilitates dependency injection and composition of capability adapters.
type Builder struct {
	stateStore     engine.StateStore
	lockManager    engine.LockManager
	eventPublisher engine.EventPublisher
	executionLog   engine.ExecutionLog
	closers        []func() error
}

// NewBuilder initializes a new DI Builder for CompositeStore.
func NewBuilder() *Builder {
	return &Builder{}
}

// WithStateStore injects the state store adapter.
func (b *Builder) WithStateStore(s engine.StateStore) *Builder {
	b.stateStore = s
	return b
}

// WithLockManager injects the distributed lock manager adapter.
func (b *Builder) WithLockManager(l engine.LockManager) *Builder {
	b.lockManager = l
	return b
}

// WithEventPublisher injects the event publisher adapter.
func (b *Builder) WithEventPublisher(p engine.EventPublisher) *Builder {
	b.eventPublisher = p
	return b
}

// WithExecutionLog injects the execution audit log adapter.
func (b *Builder) WithExecutionLog(l engine.ExecutionLog) *Builder {
	b.executionLog = l
	return b
}

// WithCloser registers a cleanup closer function to be invoked when the CompositeStore is closed.
func (b *Builder) WithCloser(closer func() error) *Builder {
	if closer != nil {
		b.closers = append(b.closers, closer)
	}
	return b
}

// Build constructs and returns the wired CompositeStore.
func (b *Builder) Build() (CompositeStore, error) {
	return NewComposite(
		b.stateStore,
		b.lockManager,
		b.eventPublisher,
		b.executionLog,
		b.closers...,
	)
}

// adapterCache manages lazy instantiation and lifecycle of backend adapters during composition.
type adapterCache struct {
	ctx      context.Context
	cfg      config.StoreConfig
	memory   *memory.Store
	redis    *redis.Store
	postgres *postgres.Store
	neo4j    *neo4j.Store
	closers  []func() error
}

func newAdapterCache(ctx context.Context, cfg config.StoreConfig) *adapterCache {
	return &adapterCache{
		ctx: ctx,
		cfg: cfg,
	}
}

func (c *adapterCache) closeAll() {
	for _, cl := range c.closers {
		if cl != nil {
			_ = cl()
		}
	}
}

func (c *adapterCache) get(driver string) (any, error) {
	switch driver {
	case "memory":
		if c.memory == nil {
			s := memory.NewStore()
			c.memory = s
			c.closers = append(c.closers, wrapCloser(s))
		}
		return c.memory, nil

	case "redis":
		if c.redis == nil {
			if c.cfg.Redis == nil || c.cfg.Redis.Addr == "" {
				return nil, errors.New("redis configuration requires addr")
			}
			s, err := redis.NewStore(redis.Config{
				Addr: c.cfg.Redis.Addr,
			})
			if err != nil {
				return nil, fmt.Errorf("initializing redis adapter: %w", err)
			}
			c.redis = s
			c.closers = append(c.closers, wrapCloser(s))
		}
		return c.redis, nil

	case "postgres":
		if c.postgres == nil {
			if c.cfg.Postgres == nil || c.cfg.Postgres.DSN == "" {
				return nil, errors.New("postgres configuration requires dsn")
			}
			s, err := postgres.New(c.ctx, postgres.Config{
				ConnString: c.cfg.Postgres.DSN,
			})
			if err != nil {
				return nil, fmt.Errorf("initializing postgres adapter: %w", err)
			}
			c.postgres = s
			c.closers = append(c.closers, wrapVoidCloser(s))
		}
		return c.postgres, nil

	case "neo4j":
		if c.neo4j == nil {
			if c.cfg.Neo4j == nil || c.cfg.Neo4j.URI == "" || c.cfg.Neo4j.User == "" || c.cfg.Neo4j.Password == "" {
				return nil, errors.New("neo4j configuration requires uri, user, and password")
			}
			s, err := neo4j.New(c.ctx, neo4j.Config{
				URI:      c.cfg.Neo4j.URI,
				User:     c.cfg.Neo4j.User,
				Password: c.cfg.Neo4j.Password,
			})
			if err != nil {
				return nil, fmt.Errorf("initializing neo4j adapter: %w", err)
			}
			c.neo4j = s
			c.closers = append(c.closers, wrapCtxCloser(s))
		}
		return c.neo4j, nil

	default:
		return nil, fmt.Errorf("unknown store driver: %s", driver)
	}
}

// New constructs and wires a CompositeStore based on the provided StoreConfig.
// It acts as the composition root for all persistence capabilities.
// If misconfigured or if backend connection fails, it fails fast and cleans up any opened resources.
func New(ctx context.Context, cfg config.StoreConfig) (CompositeStore, error) {
	cache := newAdapterCache(ctx, cfg)
	success := false
	defer func() {
		if !success {
			cache.closeAll()
		}
	}()

	stateDriver := cfg.EffectiveStateDriver()
	stateAdapter, err := cache.get(stateDriver)
	if err != nil {
		return nil, fmt.Errorf("configuring state capability with driver %q: %w", stateDriver, err)
	}
	stateStore, ok := stateAdapter.(engine.StateStore)
	if !ok {
		return nil, fmt.Errorf("driver %q does not implement engine.StateStore", stateDriver)
	}

	var lockManager engine.LockManager
	lockDriver := cfg.EffectiveLockDriver()
	if lockDriver != "" {
		if lockDriver == "neo4j" {
			return nil, errors.New("neo4j does not support capability 'lock' (allowed: memory, redis, postgres)")
		}
		lockAdapter, err := cache.get(lockDriver)
		if err != nil {
			return nil, fmt.Errorf("configuring lock capability with driver %q: %w", lockDriver, err)
		}
		lm, ok := lockAdapter.(engine.LockManager)
		if !ok {
			return nil, fmt.Errorf("driver %q does not implement engine.LockManager", lockDriver)
		}
		lockManager = lm
	}

	var eventPublisher engine.EventPublisher
	eventsDriver := cfg.EffectiveEventsDriver()
	if eventsDriver != "" {
		if eventsDriver == "neo4j" {
			return nil, errors.New("neo4j does not support capability 'events' (allowed: memory, redis, postgres)")
		}
		eventsAdapter, err := cache.get(eventsDriver)
		if err != nil {
			return nil, fmt.Errorf("configuring events capability with driver %q: %w", eventsDriver, err)
		}
		ep, ok := eventsAdapter.(engine.EventPublisher)
		if !ok {
			return nil, fmt.Errorf("driver %q does not implement engine.EventPublisher", eventsDriver)
		}
		eventPublisher = ep
	}

	var executionLog engine.ExecutionLog
	logDriver := cfg.EffectiveExecutionLogDriver()
	if logDriver != "" {
		logAdapter, err := cache.get(logDriver)
		if err != nil {
			return nil, fmt.Errorf("configuring execution_log capability with driver %q: %w", logDriver, err)
		}
		el, ok := logAdapter.(engine.ExecutionLog)
		if !ok {
			return nil, fmt.Errorf("driver %q does not implement engine.ExecutionLog", logDriver)
		}
		executionLog = el
	}

	composite, err := NewComposite(
		stateStore,
		lockManager,
		eventPublisher,
		executionLog,
		cache.closers...,
	)
	if err != nil {
		return nil, err
	}

	success = true
	return composite, nil
}

// NewFromConfig is an alias for New.
func NewFromConfig(ctx context.Context, cfg config.StoreConfig) (CompositeStore, error) {
	return New(ctx, cfg)
}
