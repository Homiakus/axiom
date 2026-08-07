package runtime

import (
	"errors"
	"fmt"
	"strings"
)

// ActivityErrorCoder can be implemented by application errors that expose a
// stable domain error code for policy catch routing.
type ActivityErrorCoder interface {
	ActivityErrorCode() string
}

// CodedActivityError is the default application-facing domain error wrapper
// used by axiom.FailActivity.
type CodedActivityError struct {
	Code  string
	Cause error
}

func (e *CodedActivityError) Error() string {
	if e == nil {
		return "activity failure"
	}
	if e.Cause == nil {
		return e.Code
	}
	if e.Code == "" {
		return e.Cause.Error()
	}
	return fmt.Sprintf("%s: %v", e.Code, e.Cause)
}

func (e *CodedActivityError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *CodedActivityError) ActivityErrorCode() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.Code)
}

func NewCodedActivityError(code string, cause error) error {
	code = strings.TrimSpace(code)
	if code == "" {
		if cause != nil {
			return cause
		}
		return errors.New("activity failure")
	}
	return &CodedActivityError{Code: code, Cause: cause}
}

func activityErrorCode(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	var coded ActivityErrorCoder
	if !errors.As(err, &coded) || coded == nil {
		return "", false
	}
	code := strings.TrimSpace(coded.ActivityErrorCode())
	return code, code != ""
}
