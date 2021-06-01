package errors

import "fmt"

const (
	NotFoundErrorCode   = "NotFound"
	NotManagedErrorCode = "NotManaged"
	OtherErrorCode      = "Other"
)

type IAMError struct {
	Code    string
	Message string
}

func (e *IAMError) Error() string {
	return fmt.Sprintf("IAMError %s: %s", e.Code, e.Message)
}

// IsNotFound is an easy way to check the only error we're really interested in: when a resource is
// not found (doesn't exist) and we need to create it.
func IsNotFound(err error) bool {
	if err, ok := err.(*IAMError); ok && err.Code == NotFoundErrorCode {
		return true
	}
	return false
}
