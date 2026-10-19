package neo4j

// Cypher query constants for Neo4j graph storage adapter.
const (
	// CypherInitSchema creates uniqueness constraints and indexes for execution entities.
	CypherInitSchema = `
CREATE CONSTRAINT execution_id_unique IF NOT EXISTS
FOR (e:Execution) REQUIRE e.execution_id IS UNIQUE;

CREATE CONSTRAINT node_state_unique IF NOT EXISTS
FOR (ns:NodeState) REQUIRE (ns.execution_id, ns.node_id) IS UNIQUE;

CREATE INDEX execution_status_idx IF NOT EXISTS
FOR (e:Execution) ON (e.status);

CREATE INDEX execution_graph_id_idx IF NOT EXISTS
FOR (e:Execution) ON (e.graph_id);
`

	// CypherGetState retrieves the JSON serialized snapshot of an execution state.
	CypherGetState = `
MATCH (e:Execution {execution_id: $execution_id})
RETURN e.state_json AS state_json, e.version AS version
`

	// CypherSetStateUpdate atomically checks expectedVersion and updates an existing execution state.
	CypherSetStateUpdate = `
MATCH (e:Execution {execution_id: $execution_id, version: $expected_version})
SET e.graph_id = $graph_id,
    e.status = $status,
    e.version = $new_version,
    e.state_json = $state_json,
    e.updated_at = datetime()
RETURN e.version AS version
`

	// CypherGetVersion retrieves the current version of an execution if it exists.
	CypherGetVersion = `
MATCH (e:Execution {execution_id: $execution_id})
RETURN e.version AS version
`

	// CypherSetStateInsert creates a new initial execution state when version is 0.
	CypherSetStateInsert = `
CREATE (e:Execution {
    execution_id: $execution_id,
    graph_id: $graph_id,
    status: $status,
    version: $new_version,
    state_json: $state_json,
    created_at: datetime(),
    updated_at: datetime()
})
RETURN e.version AS version
`

	// CypherUpsertNodeState merges an individual node state and links it to its execution.
	CypherUpsertNodeState = `
MATCH (e:Execution {execution_id: $execution_id})
MERGE (ns:NodeState {execution_id: $execution_id, node_id: $node_id})
SET ns.status = $status,
    ns.version = $version,
    ns.output_json = $output_json,
    ns.error_message = $error_message,
    ns.updated_at = datetime()
MERGE (e)-[:HAS_NODE_STATE]->(ns)
`

	// CypherCreateDependency links downstream node state as depending on upstream node state.
	CypherCreateDependency = `
MATCH (downstream:NodeState {execution_id: $execution_id, node_id: $downstream_id})
MATCH (upstream:NodeState {execution_id: $execution_id, node_id: $upstream_id})
MERGE (downstream)-[:DEPENDS_ON]->(upstream)
`

	// CypherFailureBlastRadius finds all downstream node states that depend directly or transitively
	// on a failed node state within the specified execution.
	CypherFailureBlastRadius = `
MATCH (failed:NodeState {execution_id: $execution_id, status: 'Failed'})<-[:DEPENDS_ON*1..]-(impacted:NodeState {execution_id: $execution_id})
RETURN DISTINCT impacted.node_id AS node_id, impacted.status AS status
ORDER BY node_id ASC
`

	// CypherCriticalPath calculates the longest dependency chain (critical path) of node states
	// in an execution graph, tracing from leaf nodes upstream to root nodes.
	CypherCriticalPath = `
MATCH path = (leaf:NodeState {execution_id: $execution_id})-[:DEPENDS_ON*]->(root:NodeState {execution_id: $execution_id})
WHERE NOT ()-[:DEPENDS_ON]->(leaf) AND NOT (root)-[:DEPENDS_ON]->()
RETURN [n in nodes(path) | n.node_id] AS nodes, length(path) AS depth
ORDER BY depth DESC
LIMIT 1
`

	// CypherUpstreamDependencies finds all upstream nodes that a specified node depends on directly or transitively.
	CypherUpstreamDependencies = `
MATCH (ns:NodeState {execution_id: $execution_id, node_id: $node_id})-[:DEPENDS_ON*1..]->(upstream:NodeState {execution_id: $execution_id})
RETURN DISTINCT upstream.node_id AS node_id, upstream.status AS status
ORDER BY node_id ASC
`

	// CypherAppendLog creates an append-only audit log entry node linked to the execution.
	CypherAppendLog = `
MATCH (e:Execution {execution_id: $execution_id})
CREATE (log:ExecutionLog {
    execution_id: $execution_id,
    sequence: $sequence,
    timestamp: datetime($timestamp),
    message: $message,
    details_json: $details_json
})
CREATE (e)-[:HAS_LOG]->(log)
`

	// CypherGetLogs retrieves execution logs in monotonic sequence order.
	CypherGetLogs = `
MATCH (e:Execution {execution_id: $execution_id})-[:HAS_LOG]->(log:ExecutionLog)
WHERE log.sequence >= $from_sequence
RETURN log.sequence AS sequence,
       log.timestamp AS timestamp,
       log.message AS message,
       log.details_json AS details_json
ORDER BY log.sequence ASC
LIMIT $limit
`
)
