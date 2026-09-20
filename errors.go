package typesafe

import (
	"fmt"
	"net/http"
	"time"
)

type ErrorKind string

const (
	KindHTTP                ErrorKind = "http"
	KindBadRequest          ErrorKind = "bad_request"
	KindAuthentication      ErrorKind = "authentication"
	KindPermissionDenied    ErrorKind = "permission_denied"
	KindNotFound            ErrorKind = "not_found"
	KindUnprocessableEntity ErrorKind = "unprocessable_entity"
	KindRateLimit           ErrorKind = "rate_limit"
	KindServer              ErrorKind = "server"
)

// APIError retains diagnostic data. Error intentionally excludes response bodies
// and messages, which can echo secrets. Inspect Message/Body only in a secure sink.
type APIError struct {
	Kind       ErrorKind
	StatusCode int
	Message    string
	Body       []byte
	Header     http.Header
	RequestID  string
	Endpoint   string
	RetryAfter *time.Duration
}

func (e *APIError) Error() string { return fmt.Sprintf("typesafe: HTTP %d (%s)", e.StatusCode, e.Kind) }

func kindForStatus(status int) ErrorKind {
	switch status {
	case 400:
		return KindBadRequest
	case 401:
		return KindAuthentication
	case 403:
		return KindPermissionDenied
	case 404:
		return KindNotFound
	case 422:
		return KindUnprocessableEntity
	case 429:
		return KindRateLimit
	default:
		if status >= 500 {
			return KindServer
		}
		return KindHTTP
	}
}

type ValidationError struct{ Field, Message string }

func (e *ValidationError) Error() string {
	return fmt.Sprintf("typesafe: invalid request %q: %s", e.Field, e.Message)
}

type ResponseValidationError struct {
	Field   string
	Message string
	HTTP    *HTTPResponse
}

func (e *ResponseValidationError) Error() string {
	return fmt.Sprintf("typesafe: invalid response at %q: %s", e.Field, e.Message)
}

type ConnectionError struct{ Cause error }

func (e *ConnectionError) Error() string { return "typesafe: connection or response-read failure" }
func (e *ConnectionError) Unwrap() error { return e.Cause }

type TimeoutError struct {
	Cause   error
	Timeout time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("typesafe: request timeout (%s)", e.Timeout)
}
func (e *TimeoutError) Unwrap() error { return e.Cause }

type ResponseTooLargeError struct{ Limit int64 }

func (e *ResponseTooLargeError) Error() string {
	return fmt.Sprintf("typesafe: response exceeds %d bytes", e.Limit)
}

func invalid(field, msg string) error { return &ValidationError{Field: field, Message: msg} }
func invalidResponse(field, msg string) error {
	return &ResponseValidationError{Field: field, Message: msg}
}
