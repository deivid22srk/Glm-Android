package captcha

import (
	"errors"
	"strings"
)

const (
	ErrDisabled            = "captcha_disabled"
	ErrBrowserUnavailable  = "captcha_browser_unavailable"
	ErrInteractiveRequired = "captcha_interactive_required"
	ErrTimeout             = "captcha_timeout"
	ErrEmptyToken          = "captcha_empty_token"
)

type Error struct {
	Kind    string
	Message string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func NewError(kind, message string) *Error {
	return &Error{Kind: kind, Message: message}
}

func Classify(err error) (*Error, bool) {
	if err == nil {
		return nil, false
	}
	var captchaErr *Error
	if errors.As(err, &captchaErr) {
		return captchaErr, true
	}
	value := strings.ToLower(err.Error())
	switch {
	case strings.Contains(value, "captcha bridge is disabled"):
		return NewError(ErrDisabled, err.Error()), true
	case strings.Contains(value, "fresh captcha browser is unavailable"):
		return NewError(ErrBrowserUnavailable, err.Error()), true
	case strings.Contains(value, "timed out waiting for captcha browser"):
		return NewError(ErrTimeout, err.Error()), true
	case strings.Contains(value, "interativo") || strings.Contains(value, "interactive"):
		return NewError(ErrInteractiveRequired, err.Error()), true
	case strings.Contains(value, "empty token"):
		return NewError(ErrEmptyToken, err.Error()), true
	case strings.Contains(value, "captcha"):
		return NewError("captcha_error", err.Error()), true
	default:
		return nil, false
	}
}
