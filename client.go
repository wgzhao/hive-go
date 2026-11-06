package hive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/apache/thrift/lib/go/thrift"
	"github.com/wgzhao/hive-go/internal/tcliservice"
	"github.com/wgzhao/hive-go/internal/transport"
)

// Client is a single HiveServer2 session. It is not safe for concurrent use:
// serialize Execute/Result.Next calls on one goroutine. Cancellation is done
// through the context passed to Execute.
type Client struct {
	cfg     Config
	tr      thrift.TTransport
	tc      *tcliservice.TCLIServiceClient
	session *tcliservice.TSessionHandle
	proto   tcliservice.TProtocolVersion

	closeOnce sync.Once
	closeErr  error
}

// Connect opens a TCP connection and a HiveServer2 session.
func Connect(ctx context.Context, cfg Config) (*Client, error) {
	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	c := &Client{cfg: cfg}
	if err := c.connect(ctx); err != nil {
		return nil, err
	}

	proto, fallback := requestedProtocol(cfg.Protocol)
	if err := c.openSession(ctx, proto); err != nil {
		if cfg.Auth == AuthNosASL && errors.Is(err, io.EOF) {
			err = fmt.Errorf("%w (the server may require SASL; try --auth none)", err)
		}
		if !fallback || !isProtocolError(err) {
			_ = c.disconnect()
			return nil, err
		}
		// Reconnect so the V6 handshake starts from a clean stream.
		_ = c.disconnect()
		if err := c.connect(ctx); err != nil {
			return nil, err
		}
		if err := c.openSession(ctx, tcliservice.TProtocolVersion_HIVE_CLI_SERVICE_PROTOCOL_V6); err != nil {
			_ = c.disconnect()
			return nil, err
		}
	}

	if cfg.Database != "" && !strings.EqualFold(cfg.Database, "default") {
		res, err := c.Execute(ctx, "USE "+quoteIdent(cfg.Database))
		if err != nil {
			_ = c.Close()
			return nil, fmt.Errorf("use database %q: %w", cfg.Database, err)
		}
		if err := res.Close(); err != nil {
			_ = c.Close()
			return nil, fmt.Errorf("use database %q: %w", cfg.Database, err)
		}
	}
	return c, nil
}

func (c *Client) connect(ctx context.Context) error {
	tr, err := transport.Dial(ctx, transport.Config{
		Host:           c.cfg.Host,
		Port:           c.cfg.Port,
		Auth:           transport.AuthMode(c.cfg.Auth),
		Username:       c.cfg.Username,
		Password:       c.cfg.Password,
		ConnectTimeout: c.cfg.ConnectTimeout,
		SocketTimeout:  c.cfg.SocketTimeout,
	})
	if err != nil {
		return err
	}
	c.tr = tr
	c.tc = tcliservice.NewTCLIServiceClientFactory(
		tr,
		thrift.NewTBinaryProtocolFactoryConf(&thrift.TConfiguration{}),
	)
	return nil
}

func (c *Client) disconnect() error {
	if c.tr == nil {
		return nil
	}
	err := c.tr.Close()
	c.tr = nil
	c.tc = nil
	return err
}

func (c *Client) openSession(ctx context.Context, proto tcliservice.TProtocolVersion) error {
	req := tcliservice.NewTOpenSessionReq()
	req.ClientProtocol = proto
	username := c.cfg.Username
	password := c.cfg.Password
	req.Username = &username
	req.Password = &password

	resp, err := c.tc.OpenSession(ctx, req)
	if err != nil {
		return fmt.Errorf("OpenSession: %w", err)
	}
	if err := statusError("OpenSession", resp.Status); err != nil {
		return err
	}
	if resp.SessionHandle == nil {
		return fmt.Errorf("OpenSession: server returned no session handle")
	}
	c.session = resp.SessionHandle
	c.proto = resp.GetServerProtocolVersion()
	return nil
}

// Close closes the HiveServer2 session and the TCP connection.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		var errs []string
		if c.session != nil && c.tc != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			req := tcliservice.NewTCloseSessionReq()
			req.SessionHandle = c.session
			if _, err := c.tc.CloseSession(ctx, req); err != nil {
				errs = append(errs, "CloseSession: "+err.Error())
			}
			cancel()
			c.session = nil
		}
		if err := c.disconnect(); err != nil {
			errs = append(errs, "close connection: "+err.Error())
		}
		if len(errs) > 0 {
			c.closeErr = fmt.Errorf("%s", strings.Join(errs, "; "))
		}
	})
	return c.closeErr
}

// Protocol returns the protocol version the server selected, e.g. V10.
func (c *Client) Protocol() string {
	name := c.proto.String()
	return strings.TrimPrefix(name, "HIVE_CLI_SERVICE_PROTOCOL_")
}

// Config returns a copy of the normalized client configuration.
func (c *Client) Config() Config {
	return c.cfg
}

// SetFetchSize changes the number of rows requested per FetchResults call for
// subsequent statements.
func (c *Client) SetFetchSize(n int) {
	if n > 0 {
		c.cfg.FetchSize = n
	}
}

// SetLogf enables or disables verbose diagnostic messages. Passing nil
// disables them.
func (c *Client) SetLogf(fn func(format string, args ...any)) {
	c.cfg.Logf = fn
}

func (c *Client) logf(format string, args ...any) {
	if c.cfg.Logf != nil {
		c.cfg.Logf(format, args...)
	}
}

func (c *Client) logQueryID(ctx context.Context, op *tcliservice.TOperationHandle) {
	if c.cfg.Logf == nil {
		return
	}
	req := tcliservice.NewTGetQueryIdReq()
	req.OperationHandle = op
	resp, err := c.tc.GetQueryId(ctx, req)
	if err != nil {
		return
	}
	if resp.GetQueryId() != "" {
		c.logf("Query ID: %s", resp.GetQueryId())
	}
}

func requestedProtocol(protocol string) (tcliservice.TProtocolVersion, bool) {
	switch protocol {
	case ProtocolV6:
		return tcliservice.TProtocolVersion_HIVE_CLI_SERVICE_PROTOCOL_V6, false
	case ProtocolV10:
		return tcliservice.TProtocolVersion_HIVE_CLI_SERVICE_PROTOCOL_V10, false
	default:
		return tcliservice.TProtocolVersion_HIVE_CLI_SERVICE_PROTOCOL_V10, true
	}
}

func isProtocolError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "protocol") || strings.Contains(msg, "version")
}

func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func statusError(stage string, status *tcliservice.TStatus) error {
	if status == nil {
		return fmt.Errorf("%s: server returned no status", stage)
	}
	switch status.GetStatusCode() {
	case tcliservice.TStatusCode_SUCCESS_STATUS, tcliservice.TStatusCode_SUCCESS_WITH_INFO_STATUS:
		return nil
	default:
		return &ServerError{
			Stage:    stage,
			SQLState: status.GetSqlState(),
			Code:     status.GetErrorCode(),
			Message:  status.GetErrorMessage(),
		}
	}
}

func statusInfo(status *tcliservice.TStatus) []string {
	if status == nil {
		return nil
	}
	return status.GetInfoMessages()
}
