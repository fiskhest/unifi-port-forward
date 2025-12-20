package errors

import (
	"fmt"
	"time"
)

// ErrorType represents the category of an error
type ErrorType string

const (
	ErrorTypeNotFound      ErrorType = "NOT_FOUND"
	ErrorTypeValidation    ErrorType = "VALIDATION"
	ErrorTypeRouter        ErrorType = "ROUTER"
	ErrorTypeNetwork       ErrorType = "NETWORK"
	ErrorTypeConfiguration ErrorType = "CONFIGURATION"
	ErrorTypeState         ErrorType = "STATE"
)

// ErrorSeverity indicates the impact level of an error
type ErrorSeverity string

const (
	SeverityTransient ErrorSeverity = "TRANSIENT"
	SeverityPermanent ErrorSeverity = "PERMANENT"
	SeverityWarning   ErrorSeverity = "WARNING"
)

// ControllerError is an enhanced error type that provides context and categorization
type ControllerError struct {
	Type      ErrorType              `json:"type"`
	Severity  ErrorSeverity          `json:"severity"`
	Operation string                 `json:"operation"`
	Resource  string                 `json:"resource"`
	Cause     error                  `json:"-"`
	Context   map[string]interface{} `json:"context"`
	Timestamp time.Time              `json:"timestamp"`
	Retryable bool                   `json:"retryable"`
}

// Error implements the error interface
func (e *ControllerError) Error() string {
	var base string
	if e.Resource != "" {
		base = fmt.Sprintf("%s error in %s operation on %s: %s", e.Type, e.Operation, e.Resource, e.Cause.Error())
	} else {
		base = fmt.Sprintf("%s error in %s operation: %s", e.Type, e.Operation, e.Cause.Error())
	}

	if len(e.Context) > 0 {
		base += fmt.Sprintf(" (context: %+v)", e.Context)
	}

	return base
}

// Unwrap returns the underlying cause
func (e *ControllerError) Unwrap() error {
	return e.Cause
}

// NewControllerError creates a new enhanced error
func NewControllerError(errorType ErrorType, severity ErrorSeverity, operation, resource string, cause error, context map[string]interface{}) *ControllerError {
	retryable := severity == SeverityTransient

	return &ControllerError{
		Type:      errorType,
		Severity:  severity,
		Operation: operation,
		Resource:  resource,
		Cause:     cause,
		Context:   context,
		Timestamp: time.Now(),
		Retryable: retryable,
	}
}

// NewNotFoundError creates a NOT_FOUND error with available alternatives
func NewNotFoundError(operation, resource string, cause error, searchCriteria map[string]interface{}, alternatives []Alternative) *ControllerError {
	context := make(map[string]interface{})
	for k, v := range searchCriteria {
		context[k] = v
	}
	if len(alternatives) > 0 {
		context["available_alternatives"] = alternatives
	}

	return &ControllerError{
		Type:      ErrorTypeNotFound,
		Severity:  SeverityPermanent,
		Operation: operation,
		Resource:  resource,
		Cause:     cause,
		Context:   context,
		Timestamp: time.Now(),
		Retryable: false,
	}
}

// NewValidationError creates a VALIDATION error with field-specific details
func NewValidationError(operation string, validationErrors []ValidationError) *ControllerError {
	context := map[string]interface{}{
		"validation_errors": validationErrors,
	}

	return &ControllerError{
		Type:      ErrorTypeValidation,
		Severity:  SeverityPermanent,
		Operation: operation,
		Cause:     fmt.Errorf("validation failed with %d errors", len(validationErrors)),
		Context:   context,
		Timestamp: time.Now(),
		Retryable: false,
	}
}

// Alternative represents an available option when a resource is not found
type Alternative struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Name     string                 `json:"name,omitempty"`
	Metadata map[string]interface{} `json:"metadata"`
}

// ValidationError represents a specific field validation failure
type ValidationError struct {
	Field   string `json:"field"`
	Value   string `json:"value"`
	Message string `json:"message"`
}

// IsTransient checks if an error is transient (retryable)
func IsTransient(err error) bool {
	if ctrlErr, ok := err.(*ControllerError); ok {
		return ctrlErr.Severity == SeverityTransient
	}
	return false
}

// IsPermanent checks if an error is permanent (not retryable)
func IsPermanent(err error) bool {
	if ctrlErr, ok := err.(*ControllerError); ok {
		return ctrlErr.Severity == SeverityPermanent
	}
	return false
}

// GetErrorType extracts the error type from enhanced errors
func GetErrorType(err error) ErrorType {
	if ctrlErr, ok := err.(*ControllerError); ok {
		return ctrlErr.Type
	}
	return ErrorTypeRouter // default for non-enhanced errors
}

// GetContext extracts context information from enhanced errors
func GetContext(err error) map[string]interface{} {
	if ctrlErr, ok := err.(*ControllerError); ok {
		return ctrlErr.Context
	}
	return nil
}
