// Package redis provides a Redis-backed persistence adapter for gargi-flow execution engine.
//
// It implements the following engine contracts:
//   - engine.StateStore: GraphState storage with optimistic concurrency control (CAS) via Redis Lua scripts.
//   - engine.LockManager: Distributed mutual exclusion with TTLs using SET NX PX and safe Lua release/extend.
//   - engine.EventPublisher: Execution event broadcasting using Redis Pub/Sub.
//   - engine.ExecutionLog: Append-only audit logs with monotonic sequence numbering using Redis Lists.
package redis
