package domain

import (
	"errors"
	"fmt"
)

type ErrorCode string

const (
	CodeInvalidArgument ErrorCode = "invalid_argument"
	CodeNotFound        ErrorCode = "not_found"
	CodeConflict        ErrorCode = "conflict"
	CodeForbidden       ErrorCode = "forbidden"
	CodeUnauthenticated ErrorCode = "unauthenticated"
	CodeUnavailable     ErrorCode = "unavailable"
	CodeInternal        ErrorCode = "internal"
)

// Error carries a stable machine code and a safe public detail. Cause is for
// errors.Is/errors.As and diagnostics only and must not be rendered to clients.
type Error struct {
	code      ErrorCode
	detail    string
	retryable bool
	cause     error
}

func NewError(code ErrorCode, detail string) *Error {
	if !validErrorCode(code) {
		return &Error{code: CodeInternal, detail: "internal error"}
	}
	return &Error{code: code, detail: detail}
}
func WrapError(code ErrorCode, detail string, cause error) *Error {
	err := NewError(code, detail)
	err.cause = cause
	return err
}

func (err *Error) Code() ErrorCode {
	if err == nil {
		return CodeInternal
	}
	return err.code
}
func (err *Error) Detail() string {
	if err == nil {
		return "internal error"
	}
	return err.detail
}
func (err *Error) Retryable() bool { return err != nil && err.retryable }
func (err *Error) WithRetryable() *Error {
	if err != nil {
		err.retryable = true
	}
	return err
}

func (err *Error) Error() string {
	if err == nil {
		return "<nil>"
	}
	if err.detail == "" {
		return string(err.code)
	}
	return fmt.Sprintf("%s: %s", err.code, err.detail)
}
func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

func ErrorCodeOf(err error) ErrorCode {
	var typed *Error
	if errors.As(err, &typed) {
		return typed.code
	}
	return CodeInternal
}

func validErrorCode(code ErrorCode) bool {
	switch code {
	case CodeInvalidArgument, CodeNotFound, CodeConflict, CodeForbidden, CodeUnauthenticated, CodeUnavailable, CodeInternal:
		return true
	default:
		return false
	}
}
