-- 000001_init_schema.up.sql
-- Initial schema for Gargi CKG PostgreSQL state store

CREATE TABLE IF NOT EXISTS executions (
    execution_id VARCHAR(255) PRIMARY KEY,
    graph_id VARCHAR(255) NOT NULL,
    status VARCHAR(64) NOT NULL,
    version BIGINT NOT NULL DEFAULT 0,
    state_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_executions_status ON executions (status);
CREATE INDEX IF NOT EXISTS idx_executions_graph_id ON executions (graph_id);
CREATE INDEX IF NOT EXISTS idx_executions_created_at ON executions (created_at);
CREATE INDEX IF NOT EXISTS idx_executions_updated_at ON executions (updated_at);

CREATE TABLE IF NOT EXISTS node_states (
    execution_id VARCHAR(255) NOT NULL,
    node_id VARCHAR(255) NOT NULL,
    status VARCHAR(64) NOT NULL,
    version BIGINT NOT NULL DEFAULT 0,
    output_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (execution_id, node_id)
);

CREATE INDEX IF NOT EXISTS idx_node_states_status ON node_states (status);
CREATE INDEX IF NOT EXISTS idx_node_states_updated_at ON node_states (updated_at);

CREATE TABLE IF NOT EXISTS execution_logs (
    execution_id VARCHAR(255) NOT NULL,
    sequence BIGINT NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL,
    message TEXT NOT NULL,
    level VARCHAR(32) NOT NULL DEFAULT 'INFO',
    details_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (execution_id, sequence)
);

CREATE INDEX IF NOT EXISTS idx_execution_logs_timestamp ON execution_logs (execution_id, timestamp);

CREATE TABLE IF NOT EXISTS execution_events (
    execution_id VARCHAR(255) NOT NULL,
    sequence BIGINT NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL,
    event_type VARCHAR(128) NOT NULL,
    node_id VARCHAR(255) NOT NULL DEFAULT '',
    payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (execution_id, sequence)
);

CREATE INDEX IF NOT EXISTS idx_execution_events_type ON execution_events (execution_id, event_type);

CREATE TABLE IF NOT EXISTS distributed_locks (
    resource VARCHAR(255) PRIMARY KEY,
    token VARCHAR(255) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_distributed_locks_expires_at ON distributed_locks (expires_at);
