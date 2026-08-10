// Package adapters contains infrastructure implementations of application
// ports, including HTTP, PostgreSQL, OPA, Valkey, identity, and telemetry.
//
// Adapters may depend on domain, application, and ports. Domain and application
// packages must not depend on adapters.
package adapters
