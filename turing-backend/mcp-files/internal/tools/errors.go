package tools

import (
	"errors"
	"fmt"
)

// Distinguish an observed absent-target violation from an I/O failure.
var errCreateTargetExists = errors.New("file already exists")

type InvalidParamsError struct {
	message string
}

func (e *InvalidParamsError) Error() string {
	return e.message
}

func invalidParams(message string) error {
	return &InvalidParamsError{message: message}
}

func invalidParamsf(format string, args ...any) error {
	return invalidParams(fmt.Sprintf(format, args...))
}

func IsInvalidParams(err error) bool {
	var invalid *InvalidParamsError
	return errors.As(err, &invalid)
}
