// Package simbackend defines the Backend interface through which the gargi
// CLI (and later the MCP server) runs simulation work, and Local, its
// in-process implementation.
//
// The package lives outside internal/sim so that a remote implementation can
// talk to a gargi server over HTTP without internal/sim importing an HTTP
// client (ADR-0009).
package simbackend
