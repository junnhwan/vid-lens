package service

import (
	"errors"
	"fmt"
)

type UploadSessionErrorKind string

const (
	UploadSessionErrorInvalid    UploadSessionErrorKind = "invalid"
	UploadSessionErrorNotFound   UploadSessionErrorKind = "not_found"
	UploadSessionErrorConflict   UploadSessionErrorKind = "conflict"
	UploadSessionErrorExpired    UploadSessionErrorKind = "expired"
	UploadSessionErrorInProgress UploadSessionErrorKind = "in_progress"
	UploadSessionErrorFailed     UploadSessionErrorKind = "failed"
)

// UploadSessionError carries a transport-neutral category. Handlers map the
// category to HTTP status; repository and object-store errors remain wrapped as
// causes without leaking implementation-specific strings to clients.
type UploadSessionError struct {
	Kind    UploadSessionErrorKind
	Message string
	Cause   error
}

func (e *UploadSessionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("upload session %s", e.Kind)
}

func (e *UploadSessionError) Unwrap() error { return e.Cause }

func IsUploadSessionError(err error, kind UploadSessionErrorKind) bool {
	var target *UploadSessionError
	return errors.As(err, &target) && target.Kind == kind
}

func newUploadSessionError(kind UploadSessionErrorKind, message string, cause error) error {
	return &UploadSessionError{Kind: kind, Message: message, Cause: cause}
}
