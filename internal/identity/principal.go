// Package identity defines provider-neutral authenticated identity values.
package identity

// Principal contains only identity attributes established by a trusted
// authentication adapter. Roles remain authorization inputs, not authority by
// themselves.
type Principal struct {
	ID, TenantID, Issuer string
	Roles                []string
}
