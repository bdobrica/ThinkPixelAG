# Commands

This directory contains deployable process entry points. Each command owns only
process wiring and lifecycle; business rules remain under `internal/`.

Planned commands:

- `thinkpixelag`: the governance-plane API and background workers.
- `migrate`: the PostgreSQL schema migration runner.

Command implementations are introduced by the TODO item that defines their
runtime behavior.
