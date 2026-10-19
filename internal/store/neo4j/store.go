package neo4j

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gargi-indic/gargi-ckg/pkg/engine"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

var (
	_ engine.StateStore   = (*Store)(nil)
	_ engine.ExecutionLog = (*Store)(nil)
)

// Config configures the Neo4j graph storage adapter.
type Config struct {
	// URI is the bolt or neo4j connection URI (e.g. "bolt://localhost:7687" or "neo4j://localhost:7687").
	URI string
	// User is the database authentication username.
	User string
	// Password is the database authentication password.
	Password string
	// Realm is an optional authentication realm.
	Realm string
	// Database optionally selects the specific database name (defaults to "neo4j").
	Database string
	// Executor optionally provides a custom CypherExecutor (e.g. for testing or mocking).
	Executor CypherExecutor
}

// ImpactedNode represents a node impacted by an upstream condition or failure in structural queries.
type ImpactedNode struct {
	NodeID engine.NodeID `json:"nodeId"`
	Status engine.Status `json:"status"`
}

// Store is a Neo4j-backed graph database storage adapter implementing
// engine.StateStore, engine.ExecutionLog, and structural graph queries.
type Store struct {
	exec   CypherExecutor
	closed atomic.Bool
}

// New creates and initializes a new Neo4j Store.
func New(ctx context.Context, cfg Config) (*Store, error) {
	if cfg.Executor != nil {
		if err := cfg.Executor.VerifyConnectivity(ctx); err != nil {
			return nil, wrapUnavailable(err)
		}
		return &Store{exec: cfg.Executor}, nil
	}

	if strings.TrimSpace(cfg.URI) == "" {
		return nil, errors.New("neo4j: connection URI is required")
	}

	auth := neo4jdriver.BasicAuth(cfg.User, cfg.Password, cfg.Realm)
	driver, err := neo4jdriver.NewDriverWithContext(cfg.URI, auth)
	if err != nil {
		return nil, wrapUnavailable(err)
	}

	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		return nil, wrapUnavailable(err)
	}

	dbName := cfg.Database
	if dbName == "" {
		dbName = "neo4j"
	}

	executor := newDriverExecutor(driver, dbName)
	return &Store{exec: executor}, nil
}

// NewStore is an alias for New.
func NewStore(ctx context.Context, cfg Config) (*Store, error) {
	return New(ctx, cfg)
}

// NewWithExecutor constructs a Store with an explicit CypherExecutor (e.g. for isolated testing).
func NewWithExecutor(exec CypherExecutor) *Store {
	return &Store{exec: exec}
}

// Close closes the underlying executor and connection pool.
func (s *Store) Close(ctx context.Context) error {
	if s.closed.CompareAndSwap(false, true) {
		if s.exec != nil {
			return s.exec.Close(ctx)
		}
	}
	return nil
}

func (s *Store) errClosed() error {
	return &engine.StoreUnavailableError{
		Backend: "neo4j",
		Err:     errors.New("neo4j: store is closed"),
	}
}

func (s *Store) wrapErr(err error) error {
	if err == nil {
		return nil
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return &engine.StoreUnavailableError{
			Backend: "neo4j",
			Err:     err,
		}
	}
	return err
}

func wrapUnavailable(err error) error {
	if err == nil {
		return nil
	}
	return &engine.StoreUnavailableError{
		Backend: "neo4j",
		Err:     err,
	}
}

// GetState retrieves the current GraphState snapshot for executionID.
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

	res, err := s.exec.ExecuteRead(ctx, func(tx QueryRunner) (any, error) {
		reader, err := tx.Run(ctx, CypherGetState, map[string]any{
			"execution_id": string(executionID),
		})
		if err != nil {
			return nil, err
		}
		if reader.Next(ctx) {
			rec := reader.Record()
			raw, ok := rec.Get("state_json")
			if !ok || raw == nil {
				return nil, errors.New("neo4j: missing state_json field")
			}
			return raw.(string), nil
		}
		if err := reader.Err(); err != nil {
			return nil, err
		}
		return nil, nil // not found
	})
	if err != nil {
		return nil, s.wrapErr(err)
	}

	if res == nil {
		return nil, &engine.StoreNotFoundError{
			ExecutionID: executionID,
			Entity:      "GraphState",
			Err:         engine.ErrStoreNotFound,
		}
	}

	stateJSONStr := res.(string)
	var state engine.GraphState
	if err := json.Unmarshal([]byte(stateJSONStr), &state); err != nil {
		return nil, fmt.Errorf("neo4j: failed to decode state json: %w", err)
	}

	return &state, nil
}

// SetState stores or updates the entire GraphState enforcing CAS optimistic concurrency against expectedVersion.
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

	stateBytes, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("neo4j: marshal state json: %w", err)
	}
	stateJSONStr := string(stateBytes)

	_, err = s.exec.ExecuteWrite(ctx, func(tx QueryRunner) (any, error) {
		// 1. Try atomic UPDATE where version = expectedVersion
		reader, err := tx.Run(ctx, CypherSetStateUpdate, map[string]any{
			"execution_id":     string(executionID),
			"expected_version": int64(expectedVersion),
			"new_version":      int64(state.Version()),
			"graph_id":         state.GraphID(),
			"status":           string(state.Status()),
			"state_json":       stateJSONStr,
		})
		if err != nil {
			return nil, err
		}

		updated := reader.Next(ctx)
		if err := reader.Err(); err != nil {
			return nil, err
		}

		if !updated {
			// Update did not match version or row does not exist.
			// Query current version to distinguish:
			verReader, err := tx.Run(ctx, CypherGetVersion, map[string]any{
				"execution_id": string(executionID),
			})
			if err != nil {
				return nil, err
			}

			exists := verReader.Next(ctx)
			if err := verReader.Err(); err != nil {
				return nil, err
			}

			if !exists {
				// Row does not exist
				if expectedVersion == 0 {
					// Initial creation
					insReader, err := tx.Run(ctx, CypherSetStateInsert, map[string]any{
						"execution_id": string(executionID),
						"graph_id":     state.GraphID(),
						"status":       string(state.Status()),
						"new_version":  int64(state.Version()),
						"state_json":   stateJSONStr,
					})
					if err != nil {
						return nil, err
					}
					if !insReader.Next(ctx) {
						if err := insReader.Err(); err != nil {
							return nil, err
						}
					}
				} else {
					return nil, &engine.StoreConflictError{
						ExecutionID:     executionID,
						ExpectedVersion: expectedVersion,
						ActualVersion:   0,
						Err:             engine.ErrStoreConflict,
					}
				}
			} else {
				// Row exists but version mismatched
				rec := verReader.Record()
				actualRaw, _ := rec.Get("version")
				var actualVersion uint64
				if actualRaw != nil {
					switch v := actualRaw.(type) {
					case int64:
						actualVersion = uint64(v)
					case uint64:
						actualVersion = v
					case float64:
						actualVersion = uint64(v)
					}
				}
				return nil, &engine.StoreConflictError{
					ExecutionID:     executionID,
					ExpectedVersion: expectedVersion,
					ActualVersion:   actualVersion,
					Err:             engine.ErrStoreConflict,
				}
			}
		}

		// 2. Upsert individual node states
		for _, ns := range state.NodeStates() {
			outBytes, err := json.Marshal(ns.Output())
			if err != nil {
				return nil, fmt.Errorf("neo4j: marshal node output: %w", err)
			}
			_, err = tx.Run(ctx, CypherUpsertNodeState, map[string]any{
				"execution_id":  string(executionID),
				"node_id":       string(ns.NodeID()),
				"status":        string(ns.Status()),
				"version":       int64(ns.Version()),
				"output_json":   string(outBytes),
				"error_message": ns.ErrorMessage(),
			})
			if err != nil {
				return nil, err
			}
		}

		return nil, nil
	})
	if err != nil {
		return s.wrapErr(err)
	}
	return nil
}

// PatchState atomically applies a StateMutation to the GraphState for executionID with CAS validation.
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

	res, err := s.exec.ExecuteWrite(ctx, func(tx QueryRunner) (any, error) {
		// Read current state
		reader, err := tx.Run(ctx, CypherGetState, map[string]any{
			"execution_id": string(executionID),
		})
		if err != nil {
			return nil, err
		}
		if !reader.Next(ctx) {
			if err := reader.Err(); err != nil {
				return nil, err
			}
			return nil, &engine.StoreNotFoundError{
				ExecutionID: executionID,
				Entity:      "GraphState",
				Err:         engine.ErrStoreNotFound,
			}
		}

		rec := reader.Record()
		raw, ok := rec.Get("state_json")
		if !ok || raw == nil {
			return nil, errors.New("neo4j: state_json is missing")
		}

		var current engine.GraphState
		if err := json.Unmarshal([]byte(raw.(string)), &current); err != nil {
			return nil, fmt.Errorf("neo4j: unmarshal state: %w", err)
		}

		if current.Version() != expectedVersion {
			return nil, &engine.StoreConflictError{
				ExecutionID:     executionID,
				ExpectedVersion: expectedVersion,
				ActualVersion:   current.Version(),
				Err:             engine.ErrStoreConflict,
			}
		}

		newState, err := current.Apply(*mutation)
		if err != nil {
			return nil, err
		}

		newStateBytes, err := json.Marshal(newState)
		if err != nil {
			return nil, fmt.Errorf("neo4j: marshal new state: %w", err)
		}
		newStateJSONStr := string(newStateBytes)

		// Update execution node
		updReader, err := tx.Run(ctx, CypherSetStateUpdate, map[string]any{
			"execution_id":     string(executionID),
			"expected_version": int64(expectedVersion),
			"new_version":      int64(newState.Version()),
			"graph_id":         newState.GraphID(),
			"status":           string(newState.Status()),
			"state_json":       newStateJSONStr,
		})
		if err != nil {
			return nil, err
		}
		if !updReader.Next(ctx) {
			if err := updReader.Err(); err != nil {
				return nil, err
			}
			return nil, &engine.StoreConflictError{
				ExecutionID:     executionID,
				ExpectedVersion: expectedVersion,
				ActualVersion:   current.Version(),
				Err:             engine.ErrStoreConflict,
			}
		}

		// Update the mutated node state
		ns := newState.NodeState(mutation.NodeID)
		if ns != nil {
			outBytes, err := json.Marshal(ns.Output())
			if err != nil {
				return nil, fmt.Errorf("neo4j: marshal node output: %w", err)
			}
			_, err = tx.Run(ctx, CypherUpsertNodeState, map[string]any{
				"execution_id":  string(executionID),
				"node_id":       string(ns.NodeID()),
				"status":        string(ns.Status()),
				"version":       int64(ns.Version()),
				"output_json":   string(outBytes),
				"error_message": ns.ErrorMessage(),
			})
			if err != nil {
				return nil, err
			}
		}

		return newState, nil
	})
	if err != nil {
		return nil, s.wrapErr(err)
	}
	return res.(*engine.GraphState), nil
}

// PersistGraphStructure persists graph topology as [:DEPENDS_ON] relationships
// between node states for executionID based on graph edges.
func (s *Store) PersistGraphStructure(ctx context.Context, executionID engine.ExecutionID, g *engine.Graph) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed.Load() {
		return s.errClosed()
	}
	if executionID == "" {
		return errors.New("neo4j: execution ID cannot be empty")
	}
	if g == nil {
		return errors.New("neo4j: graph cannot be nil")
	}

	_, err := s.exec.ExecuteWrite(ctx, func(tx QueryRunner) (any, error) {
		for _, edge := range g.Edges() {
			_, err := tx.Run(ctx, CypherCreateDependency, map[string]any{
				"execution_id":  string(executionID),
				"downstream_id": string(edge.To()),
				"upstream_id":   string(edge.From()),
			})
			if err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	if err != nil {
		return s.wrapErr(err)
	}
	return nil
}

// FailureBlastRadius calculates the set of downstream nodes impacted by any failed node in executionID.
func (s *Store) FailureBlastRadius(ctx context.Context, executionID engine.ExecutionID) ([]ImpactedNode, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.closed.Load() {
		return nil, s.errClosed()
	}
	if executionID == "" {
		return nil, errors.New("neo4j: execution ID cannot be empty")
	}

	res, err := s.exec.ExecuteRead(ctx, func(tx QueryRunner) (any, error) {
		reader, err := tx.Run(ctx, CypherFailureBlastRadius, map[string]any{
			"execution_id": string(executionID),
		})
		if err != nil {
			return nil, err
		}

		var impacted []ImpactedNode
		for reader.Next(ctx) {
			rec := reader.Record()
			nodeIDRaw, _ := rec.Get("node_id")
			statusRaw, _ := rec.Get("status")
			impacted = append(impacted, ImpactedNode{
				NodeID: engine.NodeID(nodeIDRaw.(string)),
				Status: engine.Status(statusRaw.(string)),
			})
		}
		if err := reader.Err(); err != nil {
			return nil, err
		}
		return impacted, nil
	})
	if err != nil {
		return nil, s.wrapErr(err)
	}
	if res == nil {
		return []ImpactedNode{}, nil
	}
	return res.([]ImpactedNode), nil
}

// CriticalPath returns the sequence of node IDs along the longest dependency chain for executionID.
func (s *Store) CriticalPath(ctx context.Context, executionID engine.ExecutionID) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.closed.Load() {
		return nil, s.errClosed()
	}
	if executionID == "" {
		return nil, errors.New("neo4j: execution ID cannot be empty")
	}

	res, err := s.exec.ExecuteRead(ctx, func(tx QueryRunner) (any, error) {
		reader, err := tx.Run(ctx, CypherCriticalPath, map[string]any{
			"execution_id": string(executionID),
		})
		if err != nil {
			return nil, err
		}

		if reader.Next(ctx) {
			rec := reader.Record()
			nodesRaw, ok := rec.Get("nodes")
			if ok && nodesRaw != nil {
				switch v := nodesRaw.(type) {
				case []string:
					return v, nil
				case []any:
					strList := make([]string, len(v))
					for i, elem := range v {
						strList[i] = fmt.Sprint(elem)
					}
					return strList, nil
				}
			}
		}
		if err := reader.Err(); err != nil {
			return nil, err
		}
		return []string{}, nil
	})
	if err != nil {
		return nil, s.wrapErr(err)
	}
	if res == nil {
		return []string{}, nil
	}
	return res.([]string), nil
}

// UpstreamDependencies returns all upstream nodes that nodeID depends on directly or transitively.
func (s *Store) UpstreamDependencies(ctx context.Context, executionID engine.ExecutionID, nodeID engine.NodeID) ([]ImpactedNode, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.closed.Load() {
		return nil, s.errClosed()
	}

	res, err := s.exec.ExecuteRead(ctx, func(tx QueryRunner) (any, error) {
		reader, err := tx.Run(ctx, CypherUpstreamDependencies, map[string]any{
			"execution_id": string(executionID),
			"node_id":      string(nodeID),
		})
		if err != nil {
			return nil, err
		}

		var deps []ImpactedNode
		for reader.Next(ctx) {
			rec := reader.Record()
			nRaw, _ := rec.Get("node_id")
			sRaw, _ := rec.Get("status")
			deps = append(deps, ImpactedNode{
				NodeID: engine.NodeID(nRaw.(string)),
				Status: engine.Status(sRaw.(string)),
			})
		}
		if err := reader.Err(); err != nil {
			return nil, err
		}
		return deps, nil
	})
	if err != nil {
		return nil, s.wrapErr(err)
	}
	if res == nil {
		return []ImpactedNode{}, nil
	}
	return res.([]ImpactedNode), nil
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

	detailsJSON, err := json.Marshal(entry.Details)
	if err != nil {
		return fmt.Errorf("neo4j: marshal details json: %w", err)
	}

	_, err = s.exec.ExecuteWrite(ctx, func(tx QueryRunner) (any, error) {
		_, err := tx.Run(ctx, CypherAppendLog, map[string]any{
			"execution_id": string(entry.ExecutionID),
			"sequence":     int64(entry.Sequence),
			"timestamp":    entry.Timestamp.Format(time.RFC3339Nano),
			"message":      entry.Message,
			"details_json": string(detailsJSON),
		})
		return nil, err
	})
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

	if limit <= 0 {
		limit = 1000
	}

	res, err := s.exec.ExecuteRead(ctx, func(tx QueryRunner) (any, error) {
		reader, err := tx.Run(ctx, CypherGetLogs, map[string]any{
			"execution_id":  string(executionID),
			"from_sequence": int64(fromSequence),
			"limit":         limit,
		})
		if err != nil {
			return nil, err
		}

		var entries []*engine.ExecutionLogEntry
		for reader.Next(ctx) {
			rec := reader.Record()
			seqRaw, _ := rec.Get("sequence")
			tsRaw, _ := rec.Get("timestamp")
			msgRaw, _ := rec.Get("message")
			detRaw, _ := rec.Get("details_json")

			var seq uint64
			if seqRaw != nil {
				switch v := seqRaw.(type) {
				case int64:
					seq = uint64(v)
				case uint64:
					seq = v
				}
			}

			var ts time.Time
			if tsRaw != nil {
				ts, _ = time.Parse(time.RFC3339Nano, fmt.Sprint(tsRaw))
			}

			var details map[string]any
			if detRaw != nil && detRaw != "" {
				_ = json.Unmarshal([]byte(fmt.Sprint(detRaw)), &details)
			}

			entries = append(entries, &engine.ExecutionLogEntry{
				ExecutionID: executionID,
				Sequence:    seq,
				Timestamp:   ts,
				Message:     fmt.Sprint(msgRaw),
				Details:     details,
			})
		}

		if err := reader.Err(); err != nil {
			return nil, err
		}
		return entries, nil
	})
	if err != nil {
		return nil, s.wrapErr(err)
	}
	if res == nil {
		return []*engine.ExecutionLogEntry{}, nil
	}
	return res.([]*engine.ExecutionLogEntry), nil
}
