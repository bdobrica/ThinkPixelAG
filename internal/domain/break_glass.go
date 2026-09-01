package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const BreakGlassContractVersion = "thinkpixelag.break-glass/v1"

type BreakGlassScope string

const (
	BreakGlassPolicyRecovery     BreakGlassScope = "POLICY_RECOVERY"
	BreakGlassRevocationRecovery BreakGlassScope = "REVOCATION_RECOVERY"
)

func (scope BreakGlassScope) Valid() bool {
	return scope == BreakGlassPolicyRecovery || scope == BreakGlassRevocationRecovery
}

type BreakGlassGrant struct {
	ID, TenantID, PrincipalID, ApprovalID ID
	Scope                                 BreakGlassScope
	ResourceType, ResourceID, ReasonCode  string
	GrantDigest, CredentialDigest         string
	StrongAuthenticationReference         string
	IssuedAt, ExpiresAt                   time.Time
}

func (grant BreakGlassGrant) Validate() error {
	if grant.ID.IsZero() || grant.TenantID.IsZero() || grant.PrincipalID.IsZero() || grant.ApprovalID.IsZero() || !grant.Scope.Valid() {
		return errors.New("break-glass identifiers and scope are invalid")
	}
	for _, value := range []string{grant.ResourceType, grant.ResourceID, grant.ReasonCode, grant.StrongAuthenticationReference} {
		if value == "" || strings.TrimSpace(value) != value {
			return errors.New("break-glass references must be non-empty and canonical")
		}
	}
	if len(grant.ResourceType) > 128 || len(grant.ResourceID) > 512 || len(grant.ReasonCode) > 128 || len(grant.StrongAuthenticationReference) > 512 || !validSHA256(grant.GrantDigest) || !validSHA256(grant.CredentialDigest) {
		return errors.New("break-glass references exceed contract bounds")
	}
	if _, err := RequireUTC(grant.IssuedAt); err != nil {
		return errors.New("break-glass issue time must be UTC")
	}
	if _, err := RequireUTC(grant.ExpiresAt); err != nil {
		return errors.New("break-glass expiry must be UTC")
	}
	if !grant.ExpiresAt.After(grant.IssuedAt) || grant.ExpiresAt.After(grant.IssuedAt.Add(15*time.Minute)) {
		return errors.New("break-glass validity must be positive and at most fifteen minutes")
	}
	return nil
}

func BreakGlassGrantDigest(tenantID, principalID ID, scope BreakGlassScope, resourceType, resourceID, reasonCode string, expiresAt time.Time) (string, error) {
	canonical := struct {
		Version      string          `json:"version"`
		TenantID     string          `json:"tenant_id"`
		PrincipalID  string          `json:"principal_id"`
		Scope        BreakGlassScope `json:"scope"`
		ResourceType string          `json:"resource_type"`
		ResourceID   string          `json:"resource_id"`
		ReasonCode   string          `json:"reason_code"`
		ExpiresAt    string          `json:"expires_at"`
	}{
		BreakGlassContractVersion, tenantID.String(), principalID.String(), scope, resourceType, resourceID, reasonCode, expiresAt.Format(time.RFC3339Nano),
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func DigestBreakGlassCredential(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func validSHA256(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(value[7:])
	return err == nil && value[7:] == strings.ToLower(value[7:])
}
