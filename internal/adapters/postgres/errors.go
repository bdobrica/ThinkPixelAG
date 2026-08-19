package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

type ErrorClass string

const (
	ErrorUnknown             ErrorClass = "unknown"
	ErrorCanceled            ErrorClass = "canceled"
	ErrorTimeout             ErrorClass = "timeout"
	ErrorUniqueViolation     ErrorClass = "unique_violation"
	ErrorForeignKeyViolation ErrorClass = "foreign_key_violation"
	ErrorCheckViolation      ErrorClass = "check_violation"
	ErrorSerialization       ErrorClass = "serialization_failure"
	ErrorDeadlock            ErrorClass = "deadlock"
	ErrorLockUnavailable     ErrorClass = "lock_unavailable"
	ErrorUnavailable         ErrorClass = "unavailable"
)

func ClassifyError(err error) ErrorClass {
	if err == nil {
		return ErrorUnknown
	}
	if errors.Is(err, context.Canceled) {
		return ErrorCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrorTimeout
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return ErrorUnknown
	}
	switch pgErr.Code {
	case "23505":
		return ErrorUniqueViolation
	case "23503":
		return ErrorForeignKeyViolation
	case "23514":
		return ErrorCheckViolation
	case "40001":
		return ErrorSerialization
	case "40P01":
		return ErrorDeadlock
	case "55P03":
		return ErrorLockUnavailable
	case "57014":
		return ErrorTimeout
	}
	if strings.HasPrefix(pgErr.Code, "08") || pgErr.Code == "57P01" || pgErr.Code == "57P02" || pgErr.Code == "57P03" {
		return ErrorUnavailable
	}
	return ErrorUnknown
}

func IsRetryable(err error) bool {
	switch ClassifyError(err) {
	case ErrorSerialization, ErrorDeadlock, ErrorLockUnavailable, ErrorUnavailable:
		return true
	}
	return false
}
