package domain

import "errors"

// ErrResourceReconciliationUnavailable means that a reconciliation scan found
// no reservation that can safely be reclaimed at the supplied instant.
var ErrResourceReconciliationUnavailable = errors.New("no resource reservation is available for reconciliation")

// ResourceReconciliationResult describes one durable reclaim. A worker may
// safely retry after receiving this result or after losing the response: the
// reservation is the exactly-once ownership boundary.
type ResourceReconciliationResult struct {
	Settlement ResourceSettlementResult
	Expired    bool
}
