// Package httperror provides constants for RFC 7807 Problem Details error responses.
package httperror

const (
	InternalTitle              = "Internal Server Error"
	InternalDetail             = "an unexpected error occurred"
	InvalidArgumentTitle       = "Invalid argument"
	InvalidArgumentMultiDetail = "multiple validation errors occurred"
	NotFoundTitle              = "Not found"
	AlreadyExistsTitle         = "Already exists"
	FailedPreconditionTitle    = "Failed precondition"
)
