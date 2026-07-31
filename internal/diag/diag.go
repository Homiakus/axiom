package diag

import (
	"fmt"
	"strings"
)

type Error struct {
	Code    string
	Message string
	File    string
	Line    int
	Kind    string
	Entity  string
	Hint    string
	Cause   error
}

func (e Error) Error() string {
	var parts []string
	if e.Code != "" {
		parts = append(parts, e.Code)
	}
	if e.File != "" && e.Line > 0 {
		parts = append(parts, fmt.Sprintf("%s:%d", e.File, e.Line))
	} else if e.Line > 0 {
		parts = append(parts, fmt.Sprintf("line %d", e.Line))
	} else if e.File != "" {
		parts = append(parts, e.File)
	}
	if e.Entity != "" {
		parts = append(parts, e.Entity)
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	if e.Hint != "" {
		parts = append(parts, "hint: "+e.Hint)
	}
	if len(parts) == 0 && e.Cause != nil {
		return e.Cause.Error()
	}
	return strings.Join(parts, ": ")
}

func (e Error) Unwrap() error {
	return e.Cause
}

func (e Error) As(target any) bool {
	if ptr, ok := target.(**Error); ok {
		copy := e
		*ptr = &copy
		return true
	}
	return false
}

type Errors []Error

func (e Errors) Error() string {
	parts := make([]string, 0, len(e))
	for _, err := range e {
		parts = append(parts, err.Error())
	}
	return strings.Join(parts, "\n")
}

func (e Errors) Unwrap() []error {
	out := make([]error, 0, len(e))
	for i := range e {
		out = append(out, e[i])
	}
	return out
}

func (e Errors) As(target any) bool {
	if len(e) == 0 {
		return false
	}
	if ptr, ok := target.(**Error); ok {
		*ptr = &e[0]
		return true
	}
	return false
}
