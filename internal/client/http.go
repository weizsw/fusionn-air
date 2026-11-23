package client

import "github.com/go-resty/resty/v2"

// NewHTTP returns a client whose callers report the final request outcome.
// Resty's retry logs can expose credentials in request URLs.
func NewHTTP() *resty.Client {
	return resty.New().SetLogger(quietLogger{})
}

type quietLogger struct{}

func (quietLogger) Errorf(string, ...any) {}
func (quietLogger) Warnf(string, ...any)  {}
func (quietLogger) Debugf(string, ...any) {}
