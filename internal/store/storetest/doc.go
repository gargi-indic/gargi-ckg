// Package storetest provides a reusable, comprehensive conformance test suite
// for pluggable engine.StateStore adapters and optional capability interfaces
// (engine.LockManager, engine.EventPublisher, engine.ExecutionLog).
//
// All storage adapter implementations (e.g., In-Memory, Redis, Postgres, Neo4j)
// must pass this test suite to guarantee semantic compliance with Gargi CKG's
// consistency model and optimistic concurrency contracts.
package storetest
