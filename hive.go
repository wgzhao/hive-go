// Package hive implements a small, dependency-light HiveServer2 client.
//
// It speaks the Thrift TCLIService protocol directly (binary transport) and
// supports NONE, NOSASL and LDAP authentication. Kerberos, HTTP transport and
// TLS are intentionally out of scope for this version.
package hive

import (
	"fmt"
	"os"
	"os/user"
	"strings"
	"time"
)

// AuthMode selects the HiveServer2 authentication mechanism.
type AuthMode string

const (
	// AuthNone uses SASL PLAIN without a server-side password check.
	AuthNone AuthMode = "none"
	// AuthNosASL skips SASL entirely.
	AuthNosASL AuthMode = "nosasl"
	// AuthLDAP uses SASL PLAIN with LDAP-backed credentials.
	AuthLDAP AuthMode = "ldap"
)

// Protocol selects the TCLIService protocol version requested from the server.
const (
	ProtocolAuto = "auto"
	ProtocolV10  = "v10"
	ProtocolV6   = "v6"
)

// Config describes a HiveServer2 connection and client defaults.
type Config struct {
	Host     string
	Port     int
	Database string
	Auth     AuthMode
	Username string
	Password string

	FetchSize int

	ConnectTimeout time.Duration
	SocketTimeout  time.Duration
	QueryTimeout   int // seconds; 0 means the server default

	// Protocol is "auto" (request V10, retry V6 on a protocol error),
	// "v10" or "v6".
	Protocol string

	// SessionConfig is sent with OpenSession and applies to the whole
	// session (for example hive.execution.engine).
	SessionConfig map[string]string

	// ConfOverlay is applied to every ExecuteStatement call. It is useful
	// for session-local settings such as hive.execution.engine.
	ConfOverlay map[string]string

	// Logf, when set, receives verbose diagnostic messages (query ID,
	// operation state and server progress). It must be safe to call from
	// the goroutine executing the statement.
	Logf func(format string, args ...any)
}

func (c *Config) normalize() error {
	if c.Host == "" {
		c.Host = "localhost"
	}
	if c.Port == 0 {
		c.Port = 10000
	}
	if c.Port < 0 || c.Port > 65535 {
		return fmt.Errorf("invalid port %d", c.Port)
	}

	c.Auth = AuthMode(strings.ToLower(string(c.Auth)))
	if c.Auth == "" {
		c.Auth = AuthNone
	}
	switch c.Auth {
	case AuthNone, AuthNosASL, AuthLDAP:
	default:
		return fmt.Errorf("unsupported auth %q (use none, nosasl or ldap)", c.Auth)
	}

	if c.Username == "" {
		c.Username = defaultUsername()
	}
	if c.Password == "" {
		if c.Auth == AuthLDAP {
			return fmt.Errorf("LDAP authentication requires a non-empty password")
		}
		// HiveServer2 with auth=none ignores the password but requires the
		// field to be present and non-empty.
		c.Password = "x"
	}

	if c.FetchSize <= 0 {
		c.FetchSize = 1000
	}
	if c.ConnectTimeout <= 0 {
		c.ConnectTimeout = 10 * time.Second
	}
	if c.SocketTimeout < 0 {
		c.SocketTimeout = 0
	}
	if c.QueryTimeout < 0 {
		c.QueryTimeout = 0
	}

	c.Protocol = strings.ToLower(strings.TrimSpace(c.Protocol))
	if c.Protocol == "" {
		c.Protocol = ProtocolAuto
	}
	switch c.Protocol {
	case ProtocolAuto, ProtocolV10, ProtocolV6:
	default:
		return fmt.Errorf("unsupported protocol %q (use auto, v10 or v6)", c.Protocol)
	}
	return nil
}

func defaultUsername() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "hive"
}

// Column describes one column in a result set.
type Column struct {
	Name    string
	Type    string // display type, e.g. INT, DECIMAL(10,2), ARRAY<STRING>
	TypeID  string // primitive TTypeId name, e.g. INT_TYPE; empty for complex types
	Comment string
}

// Kind classifies a Value for output formatting.
type Kind int

const (
	KindString Kind = iota
	KindNumber
	KindBool
	KindBinary
)

// Value is one cell of a result row. Hive returns timestamps, decimals and
// complex values as strings, so Text is the canonical representation.
type Value struct {
	Null bool
	Text string
	Kind Kind
}

// Row is one result row.
type Row []Value
