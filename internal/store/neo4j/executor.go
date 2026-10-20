package neo4j

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// CypherExecutor defines an abstraction for executing Cypher queries and managed transactions.
type CypherExecutor interface {
	// ExecuteRead runs a read unit of work within a managed transaction.
	ExecuteRead(ctx context.Context, work func(tx QueryRunner) (any, error)) (any, error)
	// ExecuteWrite runs a write unit of work within a managed transaction.
	ExecuteWrite(ctx context.Context, work func(tx QueryRunner) (any, error)) (any, error)
	// VerifyConnectivity checks that the underlying graph database is reachable.
	VerifyConnectivity(ctx context.Context) error
	// Close releases any held connections or resources.
	Close(ctx context.Context) error
}

// QueryRunner executes queries and returns results within a transaction context.
type QueryRunner interface {
	// Run executes a Cypher query with the provided parameters.
	Run(ctx context.Context, cypher string, params map[string]any) (ResultReader, error)
}

// ResultReader reads records returned by a Cypher statement.
type ResultReader interface {
	// Next advances to the next record. Returns false when exhausted or on error.
	Next(ctx context.Context) bool
	// Record returns the current record.
	Record() Record
	// Err returns the error that occurred during iteration, if any.
	Err() error
}

// Record provides key-based access to values in a query result row.
type Record interface {
	// AsMap returns the record as a map of string to value.
	AsMap() map[string]any
	// Get retrieves a field by key.
	Get(key string) (any, bool)
}

// driverExecutor wraps a live Neo4j DriverWithContext.
type driverExecutor struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func newDriverExecutor(driver neo4jdriver.DriverWithContext, database string) *driverExecutor {
	return &driverExecutor{
		driver:   driver,
		database: database,
	}
}

func (d *driverExecutor) ExecuteRead(ctx context.Context, work func(tx QueryRunner) (any, error)) (any, error) {
	session := d.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: d.database,
		AccessMode:   neo4jdriver.AccessModeRead,
	})
	defer func() { _ = session.Close(ctx) }()

	return session.ExecuteRead(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		runner := &neo4jTransactionRunner{tx: tx}
		return work(runner)
	})
}

func (d *driverExecutor) ExecuteWrite(ctx context.Context, work func(tx QueryRunner) (any, error)) (any, error) {
	session := d.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: d.database,
		AccessMode:   neo4jdriver.AccessModeWrite,
	})
	defer func() { _ = session.Close(ctx) }()

	return session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		runner := &neo4jTransactionRunner{tx: tx}
		return work(runner)
	})
}

func (d *driverExecutor) VerifyConnectivity(ctx context.Context) error {
	return d.driver.VerifyConnectivity(ctx)
}

func (d *driverExecutor) Close(ctx context.Context) error {
	return d.driver.Close(ctx)
}

type neo4jTransactionRunner struct {
	tx neo4jdriver.ManagedTransaction
}

func (r *neo4jTransactionRunner) Run(ctx context.Context, cypher string, params map[string]any) (ResultReader, error) {
	res, err := r.tx.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	return &neo4jResultReader{res: res}, nil
}

type neo4jResultReader struct {
	res neo4jdriver.ResultWithContext
}

func (nr *neo4jResultReader) Next(ctx context.Context) bool {
	return nr.res.Next(ctx)
}

func (nr *neo4jResultReader) Record() Record {
	rec := nr.res.Record()
	if rec == nil {
		return nil
	}
	return &neo4jRecord{rec: rec}
}

func (nr *neo4jResultReader) Err() error {
	return nr.res.Err()
}

type neo4jRecord struct {
	rec *neo4jdriver.Record
}

func (r *neo4jRecord) AsMap() map[string]any {
	return r.rec.AsMap()
}

func (r *neo4jRecord) Get(key string) (any, bool) {
	return r.rec.Get(key)
}

// -------------------------------------------------------------------------------------------------
// In-Memory Graph Executor for Local Testing & Environments without external Neo4j
// -------------------------------------------------------------------------------------------------

type memoryExecutionNode struct {
	executionID string
	graphID     string
	status      string
	version     uint64
	stateJSON   string
	createdAt   time.Time
	updatedAt   time.Time
}

type memoryNodeStateNode struct {
	executionID  string
	nodeID       string
	status       string
	version      uint64
	outputJSON   string
	errorMessage string
	updatedAt    time.Time
}

type inMemoryGraphExecutor struct {
	mu           sync.RWMutex
	executions   map[string]*memoryExecutionNode
	nodeStates   map[string]map[string]*memoryNodeStateNode // execID -> nodeID -> NodeState
	dependencies map[string]map[string]map[string]bool      // execID -> downstreamNodeID -> upstreamNodeID -> true
	logs         map[string][]*engine.ExecutionLogEntry
	closed       bool
}

// NewInMemoryGraphExecutor constructs a thread-safe in-memory graph engine simulating Neo4j.
func NewInMemoryGraphExecutor() CypherExecutor {
	return &inMemoryGraphExecutor{
		executions:   make(map[string]*memoryExecutionNode),
		nodeStates:   make(map[string]map[string]*memoryNodeStateNode),
		dependencies: make(map[string]map[string]map[string]bool),
		logs:         make(map[string][]*engine.ExecutionLogEntry),
	}
}

func (m *inMemoryGraphExecutor) VerifyConnectivity(_ context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return errors.New("neo4j: in-memory graph executor is closed")
	}
	return nil
}

func (m *inMemoryGraphExecutor) Close(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *inMemoryGraphExecutor) ExecuteRead(_ context.Context, work func(tx QueryRunner) (any, error)) (any, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return nil, errors.New("neo4j: in-memory graph executor is closed")
	}
	runner := &memoryQueryRunner{m: m, writeLock: false}
	return work(runner)
}

func (m *inMemoryGraphExecutor) ExecuteWrite(_ context.Context, work func(tx QueryRunner) (any, error)) (any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("neo4j: in-memory graph executor is closed")
	}
	runner := &memoryQueryRunner{m: m, writeLock: true}
	return work(runner)
}

type memoryQueryRunner struct {
	m         *inMemoryGraphExecutor
	writeLock bool
}

type memoryRecord struct {
	data map[string]any
}

func (r *memoryRecord) AsMap() map[string]any {
	return r.data
}

func (r *memoryRecord) Get(key string) (any, bool) {
	v, ok := r.data[key]
	return v, ok
}

type sliceResultReader struct {
	records []Record
	index   int
}

func (s *sliceResultReader) Next(_ context.Context) bool {
	if s.index < len(s.records) {
		s.index++
		return true
	}
	return false
}

func (s *sliceResultReader) Record() Record {
	if s.index > 0 && s.index <= len(s.records) {
		return s.records[s.index-1]
	}
	return nil
}

func (s *sliceResultReader) Err() error {
	return nil
}

func (r *memoryQueryRunner) Run(_ context.Context, cypher string, params map[string]any) (ResultReader, error) {
	switch cypher {
	case CypherGetState:
		execID, _ := params["execution_id"].(string)
		e, exists := r.m.executions[execID]
		if !exists {
			return &sliceResultReader{}, nil
		}
		return &sliceResultReader{
			records: []Record{
				&memoryRecord{data: map[string]any{
					"state_json": e.stateJSON,
					"version":    int64(e.version),
				}},
			},
		}, nil

	case CypherGetVersion:
		execID, _ := params["execution_id"].(string)
		e, exists := r.m.executions[execID]
		if !exists {
			return &sliceResultReader{}, nil
		}
		return &sliceResultReader{
			records: []Record{
				&memoryRecord{data: map[string]any{
					"version": int64(e.version),
				}},
			},
		}, nil

	case CypherSetStateUpdate:
		execID, _ := params["execution_id"].(string)
		expectedVer, _ := params["expected_version"].(int64)
		newVer, _ := params["new_version"].(int64)
		e, exists := r.m.executions[execID]
		if !exists || int64(e.version) != expectedVer {
			return &sliceResultReader{}, nil
		}
		e.graphID = params["graph_id"].(string)
		e.status = params["status"].(string)
		e.version = uint64(newVer)
		e.stateJSON = params["state_json"].(string)
		e.updatedAt = time.Now()
		return &sliceResultReader{
			records: []Record{
				&memoryRecord{data: map[string]any{"version": int64(e.version)}},
			},
		}, nil

	case CypherSetStateInsert:
		execID, _ := params["execution_id"].(string)
		if _, exists := r.m.executions[execID]; exists {
			return &sliceResultReader{}, nil
		}
		newVer, _ := params["new_version"].(int64)
		e := &memoryExecutionNode{
			executionID: execID,
			graphID:     params["graph_id"].(string),
			status:      params["status"].(string),
			version:     uint64(newVer),
			stateJSON:   params["state_json"].(string),
			createdAt:   time.Now(),
			updatedAt:   time.Now(),
		}
		r.m.executions[execID] = e
		return &sliceResultReader{
			records: []Record{
				&memoryRecord{data: map[string]any{"version": int64(e.version)}},
			},
		}, nil

	case CypherUpsertNodeState:
		execID, _ := params["execution_id"].(string)
		nodeID, _ := params["node_id"].(string)
		if r.m.nodeStates[execID] == nil {
			r.m.nodeStates[execID] = make(map[string]*memoryNodeStateNode)
		}
		ver, _ := params["version"].(int64)
		r.m.nodeStates[execID][nodeID] = &memoryNodeStateNode{
			executionID:  execID,
			nodeID:       nodeID,
			status:       params["status"].(string),
			version:      uint64(ver),
			outputJSON:   params["output_json"].(string),
			errorMessage: params["error_message"].(string),
			updatedAt:    time.Now(),
		}
		return &sliceResultReader{}, nil

	case CypherCreateDependency:
		execID, _ := params["execution_id"].(string)
		downstream, _ := params["downstream_id"].(string)
		upstream, _ := params["upstream_id"].(string)
		if r.m.dependencies[execID] == nil {
			r.m.dependencies[execID] = make(map[string]map[string]bool)
		}
		if r.m.dependencies[execID][downstream] == nil {
			r.m.dependencies[execID][downstream] = make(map[string]bool)
		}
		r.m.dependencies[execID][downstream][upstream] = true
		return &sliceResultReader{}, nil

	case CypherFailureBlastRadius:
		execID, _ := params["execution_id"].(string)
		nodes := r.m.nodeStates[execID]
		deps := r.m.dependencies[execID]
		if nodes == nil {
			return &sliceResultReader{}, nil
		}

		// Find failed nodes
		failedNodes := make(map[string]bool)
		for id, ns := range nodes {
			if ns.status == string(engine.StatusFailed) {
				failedNodes[id] = true
			}
		}

		// Find all downstream nodes that depend on any failed node directly or transitively
		impacted := make(map[string]string)
		for downstreamID := range nodes {
			if failedNodes[downstreamID] {
				continue
			}
			if r.isDownstreamOfAnyFailed(downstreamID, failedNodes, deps, make(map[string]bool)) {
				impacted[downstreamID] = nodes[downstreamID].status
			}
		}

		var records []Record
		sortedKeys := make([]string, 0, len(impacted))
		for k := range impacted {
			sortedKeys = append(sortedKeys, k)
		}
		sort.Strings(sortedKeys)
		for _, k := range sortedKeys {
			records = append(records, &memoryRecord{
				data: map[string]any{
					"node_id": k,
					"status":  impacted[k],
				},
			})
		}
		return &sliceResultReader{records: records}, nil

	case CypherCriticalPath:
		execID, _ := params["execution_id"].(string)
		nodes := r.m.nodeStates[execID]
		deps := r.m.dependencies[execID]
		if len(nodes) == 0 {
			return &sliceResultReader{}, nil
		}

		// Find leaves (nodes with no downstream nodes depending on them)
		isUpstreamOf := make(map[string]bool)
		for _, upMap := range deps {
			for up := range upMap {
				isUpstreamOf[up] = true
			}
		}

		var leaves []string
		for id := range nodes {
			if !isUpstreamOf[id] {
				leaves = append(leaves, id)
			}
		}
		sort.Strings(leaves)

		var longestPath []any
		for _, leaf := range leaves {
			path := r.findLongestPathFrom(leaf, deps)
			if len(path) > len(longestPath) {
				longestPath = path
			}
		}

		if len(longestPath) == 0 {
			for id := range nodes {
				longestPath = []any{id}
				break
			}
		}

		return &sliceResultReader{
			records: []Record{
				&memoryRecord{data: map[string]any{
					"nodes": longestPath,
					"depth": int64(len(longestPath)),
				}},
			},
		}, nil

	case CypherUpstreamDependencies:
		execID, _ := params["execution_id"].(string)
		nodeID, _ := params["node_id"].(string)
		nodes := r.m.nodeStates[execID]
		deps := r.m.dependencies[execID]
		if nodes == nil || deps == nil {
			return &sliceResultReader{}, nil
		}

		visited := make(map[string]bool)
		r.collectUpstream(nodeID, deps, visited)

		var records []Record
		sorted := make([]string, 0, len(visited))
		for k := range visited {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			status := "Pending"
			if ns, ok := nodes[k]; ok {
				status = ns.status
			}
			records = append(records, &memoryRecord{
				data: map[string]any{
					"node_id": k,
					"status":  status,
				},
			})
		}
		return &sliceResultReader{records: records}, nil

	case CypherAppendLog:
		execID, _ := params["execution_id"].(string)
		seq, _ := params["sequence"].(int64)
		detailsRaw, _ := params["details_json"].(string)
		var details map[string]any
		if detailsRaw != "" {
			_ = json.Unmarshal([]byte(detailsRaw), &details)
		}
		ts, _ := params["timestamp"].(string)
		parsedTime, _ := time.Parse(time.RFC3339Nano, ts)
		entry := &engine.ExecutionLogEntry{
			ExecutionID: engine.ExecutionID(execID),
			Sequence:    uint64(seq),
			Timestamp:   parsedTime,
			Message:     params["message"].(string),
			Details:     details,
		}
		r.m.logs[execID] = append(r.m.logs[execID], entry)
		return &sliceResultReader{}, nil

	case CypherGetLogs:
		execID, _ := params["execution_id"].(string)
		fromSeq, _ := params["from_sequence"].(int64)
		limit, _ := params["limit"].(int)
		entries := r.m.logs[execID]
		var records []Record
		for _, e := range entries {
			if int64(e.Sequence) >= fromSeq {
				detailsBytes, _ := json.Marshal(e.Details)
				records = append(records, &memoryRecord{
					data: map[string]any{
						"sequence":     int64(e.Sequence),
						"timestamp":    e.Timestamp.Format(time.RFC3339Nano),
						"message":      e.Message,
						"details_json": string(detailsBytes),
					},
				})
				if limit > 0 && len(records) >= limit {
					break
				}
			}
		}
		return &sliceResultReader{records: records}, nil

	default:
		// Unknown statement returns empty reader
		return &sliceResultReader{}, nil
	}
}

func (r *memoryQueryRunner) isDownstreamOfAnyFailed(curr string, failed map[string]bool, deps map[string]map[string]bool, visited map[string]bool) bool {
	if visited[curr] {
		return false
	}
	visited[curr] = true

	upMap := deps[curr]
	for up := range upMap {
		if failed[up] {
			return true
		}
		if r.isDownstreamOfAnyFailed(up, failed, deps, visited) {
			return true
		}
	}
	return false
}

func (r *memoryQueryRunner) findLongestPathFrom(curr string, deps map[string]map[string]bool) []any {
	upMap := deps[curr]
	if len(upMap) == 0 {
		return []any{curr}
	}

	var bestUpstream []any
	for up := range upMap {
		candidate := r.findLongestPathFrom(up, deps)
		if len(candidate) > len(bestUpstream) {
			bestUpstream = candidate
		}
	}

	result := []any{curr}
	return append(result, bestUpstream...)
}

func (r *memoryQueryRunner) collectUpstream(curr string, deps map[string]map[string]bool, visited map[string]bool) {
	upMap := deps[curr]
	for up := range upMap {
		if !visited[up] {
			visited[up] = true
			r.collectUpstream(up, deps, visited)
		}
	}
}
