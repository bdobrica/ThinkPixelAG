package ports

import "context"

// HarnessSnapshot contains authoritative revisions and composition facts, never
// descriptions, credentials, topology, prompts or peer-supplied instructions.
type HarnessSnapshot struct {
	PolicyDigest          string
	PolicyVersion         int64
	MappingRevision       int64
	ConfigurationRevision string
	RunList               bool
}
type HarnessStateReader interface {
	HarnessSnapshot(context.Context) (HarnessSnapshot, error)
}
