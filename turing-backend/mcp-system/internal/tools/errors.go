package tools

import (
	"errors"
	"fmt"
)

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
