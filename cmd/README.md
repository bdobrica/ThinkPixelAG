# Commands

This directory contains deployable process entry points. Each command owns only
process wiring and lifecycle; business rules remain under `internal/`.

Commands:

- `thinkpixelag`: implemented governance-plane API process wiring, operational
  endpoints, observability ownership, and graceful lifecycle. Business routes
  and background workers are added by their feature phases.
- `migrate`: planned PostgreSQL schema migration runner.

Command implementations are introduced by the TODO item that defines their
runtime behavior.
