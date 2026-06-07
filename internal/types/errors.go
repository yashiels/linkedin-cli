package types

import "fmt"

// Exit code constants used throughout lnk.
const (
	ExitOK        = 0 // Success
	ExitGeneral   = 1 // General / unclassified error
	ExitUsage     = 2 // Bad flag, missing argument, or misuse
	ExitAuth      = 3 // Authentication / credential error
	ExitNetwork   = 4 // Network or HTTP transport error
	ExitRateLimit = 5 // Rate-limited by LinkedIn (HTTP 429)
)

// LnkError is an error that carries an exit code.
type LnkError struct {
	Code    int
	Message string
	Cause   error
}

func (e *LnkError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *LnkError) Unwrap() error { return e.Cause }

// NewError creates an LnkError with the given exit code and message.
func NewError(code int, msg string) *LnkError {
	return &LnkError{Code: code, Message: msg}
}

// WrapError wraps cause with an exit code and message.
func WrapError(code int, msg string, cause error) *LnkError {
	return &LnkError{Code: code, Message: msg, Cause: cause}
}

// AuthError returns an ExitAuth-coded error.
func AuthError(msg string) *LnkError { return NewError(ExitAuth, msg) }

// NetworkError returns an ExitNetwork-coded error.
func NetworkError(msg string, cause error) *LnkError { return WrapError(ExitNetwork, msg, cause) }

// RateLimitError returns an ExitRateLimit-coded error.
func RateLimitError() *LnkError {
	return NewError(ExitRateLimit, "rate limited by LinkedIn (HTTP 429)")
}
