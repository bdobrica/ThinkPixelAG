package ports

import (
	"context"
	"time"
)

type RoleMappings struct {
	Mode      string            `json:"mode"`
	Issuer    string            `json:"issuer"`
	Revision  int64             `json:"revision"`
	Mappings  map[string]string `json:"mappings"`
	UpdatedAt time.Time         `json:"updated_at,omitempty"`
}
type RoleMappingStore interface {
	RoleMappings(context.Context, string) (RoleMappings, error)
	SaveRoleMappings(context.Context, AdministrationOperation, RoleMappings, string) (RoleMappings, error)
}
