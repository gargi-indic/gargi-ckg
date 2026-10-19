-- 000001_init_schema.down.sql
-- Revert initial schema for Gargi CKG PostgreSQL state store

DROP TABLE IF EXISTS distributed_locks CASCADE;
DROP TABLE IF EXISTS execution_events CASCADE;
DROP TABLE IF EXISTS execution_logs CASCADE;
DROP TABLE IF EXISTS node_states CASCADE;
DROP TABLE IF EXISTS executions CASCADE;
