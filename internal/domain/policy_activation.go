package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Four-eyes action digest uses explicit field names and fixed serialization.
func PolicyActivationDigest(tenant ID, channel, digest string, expected int64) string {
	raw, _ := json.Marshal(struct {
		Tenant  string `json:"tenant_id"`
		Channel string `json:"channel"`
		Digest  string `json:"policy_digest"`
		Version int64  `json:"expected_policy_epoch"`
	}{tenant.String(), channel, digest, expected})
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
