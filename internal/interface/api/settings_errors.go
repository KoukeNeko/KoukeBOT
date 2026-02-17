package api

import (
	"errors"
	"fmt"
)

type settingsClientError struct {
	message string
}

func (e *settingsClientError) Error() string {
	return e.message
}

func newSettingsClientError(format string, args ...interface{}) error {
	return &settingsClientError{
		message: fmt.Sprintf(format, args...),
	}
}

func isSettingsClientError(err error) bool {
	var target *settingsClientError
	return errors.As(err, &target)
}

