package diet

import (
	"fmt"
	"net/http"
)

// Error is the business error type surfaced to HTTP clients as a 400
// response with a JSON message body. Status optionally overrides the
// rendered HTTP status (for example 404 for missing resources); zero
// means the default 400.
type Error struct {
	Message string
	Cause   error
	Status  int
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

// Unwrap exposes the wrapped cause.
func (e *Error) Unwrap() error { return e.Cause }

// NewError builds a business error with a formatted message.
func NewError(format string, args ...any) *Error {
	return &Error{Message: fmt.Sprintf(format, args...)}
}

// NewNotFoundError builds a business error rendered as HTTP 404.
func NewNotFoundError(format string, args ...any) *Error {
	return &Error{Message: fmt.Sprintf(format, args...), Status: http.StatusNotFound}
}

// WrapError builds a business error wrapping a cause.
func WrapError(cause error, format string, args ...any) *Error {
	return &Error{Message: fmt.Sprintf(format, args...), Cause: cause}
}

// AsError reports whether err is a business Error and returns it.
func AsError(err error) (*Error, bool) {
	if err == nil {
		return nil, false
	}
	target, ok := err.(*Error)
	return target, ok
}
