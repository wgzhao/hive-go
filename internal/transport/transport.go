// Package transport establishes the binary Thrift transport used by
// HiveServer2. It implements the SASL PLAIN handshake for NONE/LDAP and the
// plain (NOSASL) mode.
package transport

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/apache/thrift/lib/go/thrift"
)

// AuthMode selects how the client authenticates to HiveServer2.
type AuthMode string

const (
	// AuthNone performs the SASL PLAIN handshake without a server-side
	// password check. Hive still requires a non-empty password field.
	AuthNone AuthMode = "none"
	// AuthNosASL skips SASL entirely.
	AuthNosASL AuthMode = "nosasl"
	// AuthLDAP performs the SASL PLAIN handshake against an LDAP-backed
	// HiveServer2.
	AuthLDAP AuthMode = "ldap"
)

const (
	defaultHandshakeTimeout = 15 * time.Second
	defaultMaxFrameSize     = 64 * 1024 * 1024
)

// Config describes a HiveServer2 connection.
type Config struct {
	Host           string
	Port           int
	Auth           AuthMode
	Username       string
	Password       string
	ConnectTimeout time.Duration
	SocketTimeout  time.Duration
	MaxFrameSize   int32
}

// Address returns the host:port dial address.
func (c Config) Address() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

// Dial connects to HiveServer2 and returns a Thrift transport ready for the
// TCLIService protocol. The caller owns the returned transport and must close
// it.
func Dial(ctx context.Context, cfg Config) (thrift.TTransport, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("transport: empty host")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("transport: invalid port %d", cfg.Port)
	}
	switch cfg.Auth {
	case AuthNone, AuthLDAP, AuthNosASL:
	default:
		return nil, fmt.Errorf("transport: unsupported auth mode %q (supported: none, nosasl, ldap)", cfg.Auth)
	}

	dialer := &net.Dialer{Timeout: cfg.ConnectTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", cfg.Address())
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", cfg.Address(), err)
	}

	maxFrame := cfg.MaxFrameSize
	if maxFrame <= 0 {
		maxFrame = defaultMaxFrameSize
	}
	conf := &thrift.TConfiguration{
		ConnectTimeout: cfg.ConnectTimeout,
		SocketTimeout:  cfg.SocketTimeout,
		MaxFrameSize:   maxFrame,
		MaxMessageSize: maxFrame,
	}
	base := thrift.NewTSocketFromConnConf(conn, conf)

	if cfg.Auth == AuthNosASL {
		return thrift.NewTBufferedTransport(base, 8192), nil
	}

	handshakeTimeout := cfg.ConnectTimeout
	if handshakeTimeout <= 0 {
		handshakeTimeout = defaultHandshakeTimeout
	}
	_ = conn.SetDeadline(time.Now().Add(handshakeTimeout))
	if err := saslPlainHandshake(conn, cfg.Username, cfg.Password); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w (if the server is configured for NOSASL, use --auth nosasl)", err)
	}
	_ = conn.SetDeadline(time.Time{})

	// After a PLAIN handshake there is no SASL security layer: Thrift
	// messages are standard framed messages.
	return thrift.NewTFramedTransportConf(base, conf), nil
}
