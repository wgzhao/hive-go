package transport

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

// SASL message status codes used by the Thrift SASL framing.
const (
	saslStart    = 1
	saslOK       = 2
	saslBad      = 3
	saslError    = 4
	saslComplete = 5
)

const maxSASLMessageSize = 1 << 20

// saslPlainHandshake performs the Thrift SASL PLAIN negotiation on conn.
// The PLAIN mechanism sends "\x00username\x00password" as the initial
// response. After the handshake there is no security layer, so subsequent
// Thrift data is framed normally.
func saslPlainHandshake(conn net.Conn, username, password string) error {
	if username == "" {
		return fmt.Errorf("SASL PLAIN: empty username")
	}
	if password == "" {
		return fmt.Errorf("SASL PLAIN: empty password (HiveServer2 with auth=none still requires a non-empty value, usually \"x\")")
	}

	// Thrift's Java server reads START, creates the SaslServer, and then
	// immediately waits for the client's initial response. It does not send
	// an intermediate challenge, so START and the initial response must be
	// sent back-to-back. This matches the reference gohive client.
	if err := writeSASLMessage(conn, saslStart, []byte("PLAIN")); err != nil {
		return fmt.Errorf("SASL PLAIN: send START: %w", err)
	}

	initial := make([]byte, 0, len(username)+len(password)+2)
	initial = append(initial, 0)
	initial = append(initial, username...)
	initial = append(initial, 0)
	initial = append(initial, password...)
	if err := writeSASLMessage(conn, saslOK, initial); err != nil {
		return fmt.Errorf("SASL PLAIN: send initial response: %w", err)
	}

	for {
		status, body, err := readSASLMessage(conn)
		if err != nil {
			return fmt.Errorf("SASL PLAIN: read authentication response: %w", err)
		}
		switch status {
		case saslComplete:
			return nil
		case saslOK:
			// PLAIN has no challenge/response exchange after the initial
			// response. Reply with an empty response to keep the state
			// machine moving; real HiveServer2 normally sends COMPLETE
			// directly.
			if err := writeSASLMessage(conn, saslOK, nil); err != nil {
				return fmt.Errorf("SASL PLAIN: send challenge response: %w", err)
			}
		case saslBad, saslError:
			return saslFailure("authentication", status, body)
		default:
			return fmt.Errorf("SASL PLAIN: unexpected status %d", status)
		}
	}
}

func saslFailure(stage string, status byte, body []byte) error {
	msg := string(body)
	if len(msg) > 512 {
		msg = msg[:512] + "..."
	}
	if msg == "" {
		return fmt.Errorf("SASL PLAIN %s failed (status %d)", stage, status)
	}
	return fmt.Errorf("SASL PLAIN %s failed (status %d): %s", stage, status, msg)
}

func writeSASLMessage(w io.Writer, status byte, body []byte) error {
	header := make([]byte, 5)
	header[0] = status
	binary.BigEndian.PutUint32(header[1:], uint32(len(body)))
	if err := writeAll(w, header); err != nil {
		return err
	}
	return writeAll(w, body)
}

func readSASLMessage(r io.Reader) (byte, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	size := binary.BigEndian.Uint32(header[1:])
	if size > maxSASLMessageSize {
		return 0, nil, fmt.Errorf("SASL message too large: %d bytes", size)
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, nil, err
	}
	return header[0], body, nil
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
