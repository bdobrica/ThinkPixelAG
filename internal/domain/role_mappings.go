package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
)

var externalRoleName = regexp.MustCompile(`^[A-Za-z0-9._:/@-]{1,128}$`)

// ValidateRoleMappings excludes workload identities and prevents configuration
// lockout. IdP membership remains outside AG's authority.
func ValidateRoleMappings(m map[string]string) error {
	if len(m) == 0 || len(m) > 256 {
		return NewError(CodeInvalidArgument, "between 1 and 256 role mappings required")
	}
	admin := false
	for external, role := range m {
		if !externalRoleName.MatchString(external) {
			return NewError(CodeInvalidArgument, "invalid external role name")
		}
		switch role {
		case "policy-admin":
			admin = true
		case "agent-invoker", "registry-admin", "resource-admin", "revocation-admin":
		default:
			return NewError(CodeInvalidArgument, "unsupported OIDC role")
		}
	}
	if !admin {
		return NewError(CodeConflict, "last policy-administrator mapping cannot be removed")
	}
	return nil
}
func RoleMappingExpansion(before, after map[string]string) bool {
	for external, role := range after {
		if role != "agent-invoker" && before[external] != role {
			return true
		}
	}
	return false
}
func RoleMappingDigest(tenant ID, issuer string, revision int64, mappings map[string]string) string {
	b, _ := json.Marshal(struct {
		Tenant   string            `json:"tenant_id"`
		Issuer   string            `json:"issuer"`
		Revision int64             `json:"expected_revision"`
		Mappings map[string]string `json:"mappings"`
	}{tenant.String(), issuer, revision, mappings})
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
