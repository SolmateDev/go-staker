package errormsg

import (
	"fmt"
	"net/http"
)

type ErrorMessage struct {
	isProd bool
}

func CreateFromEnv() ErrorMessage {
	isProd := true

	return ErrorMessage{isProd: isProd}
}

func (em ErrorMessage) Error(msg error) error {

	if em.isProd {
		code := http.StatusInternalServerError
		return fmt.Errorf("%d: %s", code, http.StatusText(code))
	} else {
		return msg
	}
}

func (em ErrorMessage) ErrorWithCode(code int, message error) error {
	if em.isProd {
		return fmt.Errorf("%d: %s", code, http.StatusText(code))
	} else {
		return message
	}
}

func (em ErrorMessage) Code(code int) error {
	return fmt.Errorf("%d: %s", code, http.StatusText(code))
}
