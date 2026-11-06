package hive

import (
	"errors"
	"fmt"
)

var (
	// ErrCanceled is returned when a query is canceled through its context.
	ErrCanceled = errors.New("query canceled")
	// ErrClosed is returned when an operation is attempted on a closed client.
	ErrClosed = errors.New("client is closed")
)

// ServerError is a SQL or protocol error reported by HiveServer2.
type ServerError struct {
	Stage    string
	SQLState string
	Code     int32
	Message  string
}

func (e *ServerError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = "server error"
	}
	if e.SQLState != "" && e.Code != 0 {
		return fmt.Sprintf("%s: %s (sqlState=%s, errorCode=%d)", e.Stage, msg, e.SQLState, e.Code)
	}
	if e.SQLState != "" {
		return fmt.Sprintf("%s: %s (sqlState=%s)", e.Stage, msg, e.SQLState)
	}
	if e.Code != 0 {
		return fmt.Sprintf("%s: %s (errorCode=%d)", e.Stage, msg, e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Stage, msg)
}
