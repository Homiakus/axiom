// Package profile publishes the three canonical integration profiles for Axiom:
// Embedded, Durable Single Node, and Distributed Production.
//
// These profiles provide opinionated, turnkey entry points that assemble
// the appropriate stores, engines, durability levels, and coordination
// mechanisms for common deployment topologies without merging the distinct
// underlying engines.
//
// # Profile Overview
//
// 1. Embedded:
//   - Best for: unit testing, CLI utilities, embedded systems, microsecond in-process workflows.
//   - Persistence: Ephemeral (in-memory).
//   - Infrastructure: Zero dependencies, zero disk writes, zero daemons.
//
// 2. Durable Single Node:
//   - Best for: standalone microservices, durable agents, local daemons requiring crash restart recovery.
//   - Persistence: Synchronous Pebble WAL (StoreDurabilitySynchronous).
//   - Infrastructure: Local filesystem directory, single-writer process lock.
//
// 3. Distributed Production:
//   - Best for: distributed architectures, separate coordinator and worker pools, cluster deployments.
//   - Persistence: Shared transactional store and control-plane stores.
//   - Infrastructure: Leased fencing, adaptive provider routing, admission control, worker health heartbeats.
package profile
