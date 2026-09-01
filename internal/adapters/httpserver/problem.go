package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type Problem struct {
	Type              string `json:"type"`
	Title             string `json:"title"`
	Status            int    `json:"status"`
	Detail            string `json:"detail,omitempty"`
	Code              string `json:"code"`
	RequestID         string `json:"request_id"`
	RetryAfterSeconds *int   `json:"retry_after_seconds,omitempty"`
}

func problemFor(status int, code, title, detail string) Problem {
	return Problem{Type: "/problems/" + code, Title: title, Status: status, Detail: detail, Code: code}
}

func ProblemFromError(err error) Problem {
	var typed *domain.Error
	if !errors.As(err, &typed) {
		return problemFor(http.StatusInternalServerError, "internal", "Internal Server Error", "An internal error occurred.")
	}
	status, title := http.StatusInternalServerError, "Internal Server Error"
	switch typed.Code() {
	case domain.CodeInvalidArgument:
		status, title = http.StatusBadRequest, "Invalid Request"
	case domain.CodeUnauthenticated:
		status, title = http.StatusUnauthorized, "Unauthenticated"
	case domain.CodeForbidden:
		status, title = http.StatusForbidden, "Forbidden"
	case domain.CodeNotFound:
		status, title = http.StatusNotFound, "Not Found"
	case domain.CodeConflict:
		status, title = http.StatusConflict, "Conflict"
	case domain.CodeUnavailable:
		status, title = http.StatusServiceUnavailable, "Service Unavailable"
	case domain.CodeInternal:
		return problemFor(http.StatusInternalServerError, "internal", "Internal Server Error", "An internal error occurred.")
	}
	problem := problemFor(status, string(typed.Code()), title, safeProblemDetail(typed.Detail()))
	if typed.Retryable() {
		zero := 0
		problem.RetryAfterSeconds = &zero
	}
	return problem
}

func safeProblemDetail(detail string) string {
	normalized := strings.ToLower(detail)
	if detail == "" || len(detail) > 512 || strings.ContainsAny(detail, "\r\n\x00") {
		return "The request could not be completed."
	}
	for _, restricted := range []string{"secret", "password", "credential", "authorization", "cookie", "objective", "prompt", "payload", "database url", "valkey url", "private key"} {
		if strings.Contains(normalized, restricted) {
			return "The request could not be completed."
		}
	}
	return detail
}

func writeProblem(writer http.ResponseWriter, request *http.Request, problem Problem) {
	problem.RequestID = requestIDFromContext(request.Context())
	writer.Header().Set("Content-Type", "application/problem+json")
	if problem.RetryAfterSeconds != nil {
		writer.Header().Set("Retry-After", fmt.Sprintf("%d", *problem.RetryAfterSeconds))
	}
	writer.WriteHeader(problem.Status)
	_ = encodeJSON(writer, problem)
}

func encodeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(true)
	return encoder.Encode(value)
}

// DecodeJSON decodes one bounded JSON value and rejects trailing content.
func DecodeJSON(request *http.Request, destination any) error {
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return domain.NewError(domain.CodeInvalidArgument, "request body exceeds the configured limit")
		}
		return domain.NewError(domain.CodeInvalidArgument, "request body must be valid JSON")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return domain.NewError(domain.CodeInvalidArgument, "request body must contain exactly one JSON value")
	}
	return nil
}
