# ADR-0010: Model Context Protocol (MCP) Server Go SDK

## Title
Use the official Go MCP SDK (`github.com/modelcontextprotocol/go-sdk`) for the MCP server.

## Status
Accepted

## Date
2026-10-24

## Context
We are implementing an MCP (Model Context Protocol) server to expose tools and resources (schemas, templates) to model building agents (e.g., Claude) in issue #144. We need a library to handle the MCP protocol (JSON-RPC over stdio and streamable HTTP).

## Decision
Use the official Go MCP SDK `github.com/modelcontextprotocol/go-sdk`, pinned at exactly `v1.8.0`.

## Consequences
- We get a standard, maintained implementation of the MCP protocol, supporting both stdio and SSE/HTTP streams out of the box.
- We add a new third-party dependency to the core CLI binaries (`cmd/gargi`).
- We can quickly expose our APIs (like schema retrieval and template listing) as MCP tools and resources without worrying about low-level JSON-RPC parsing, message routing, or transport protocols.

## Alternatives
- **In-house JSON-RPC:** We could have implemented a custom JSON-RPC router for stdio and HTTP. We rejected this because the MCP protocol has specific message types, capabilities negotiation, and transport semantics (especially for streamable HTTP/SSE) that are non-trivial to implement correctly and maintain. Using the official SDK reduces maintenance burden and ensures compliance with the spec.
