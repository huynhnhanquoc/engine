package host

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// portClient is the least of a PostgreSQL client kitwork migrate needs to change a file a server holds through the
// server's database port (docs/KITWORK_DATABASE.md, "Database là gốc", B2): the startup, SCRAM-SHA-256 (the port's
// only way in), and the simple query protocol. engine2 keeps no driver (the old engine used lib/pq), so it is written
// here, the standard library only.
type portClient struct {
	conn   net.Conn
	reader *bufio.Reader
}

// portError is the port's ErrorResponse: its SQLSTATE and message.
type portError struct{ code, message string }

func (e *portError) Error() string { return e.message + " (" + e.code + ")" }

// dialPort opens a session as user on database at address.
func dialPort(ctx context.Context, address, user, password, database string) (*portClient, error) {
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	c := &portClient{conn: conn, reader: bufio.NewReader(conn)}
	startup := []byte{0, 0, 0, 0, 0, 3, 0, 0} // length, protocol 3.0
	for _, pair := range [][2]string{{"user", user}, {"database", database}, {"application_name", "kitwork migrate"}} {
		startup = append(append(append(append(startup, pair[0]...), 0), pair[1]...), 0)
	}
	startup = append(startup, 0)
	binary.BigEndian.PutUint32(startup, uint32(len(startup)))
	if _, err := conn.Write(startup); err != nil {
		conn.Close()
		return nil, err
	}
	if err := c.authenticate(password); err != nil {
		conn.Close()
		return nil, err
	}
	return c, nil
}

func (c *portClient) close() {
	_ = c.send('X', nil) // Terminate
	_ = c.conn.Close()
}

func (c *portClient) send(kind byte, payload []byte) error {
	message := make([]byte, 5, 5+len(payload))
	message[0] = kind
	binary.BigEndian.PutUint32(message[1:], uint32(4+len(payload)))
	_, err := c.conn.Write(append(message, payload...))
	return err
}

// maxMessage bounds a message the port sends: what migrate reads is tags and errors.
const maxMessage = 1 << 24

func (c *portClient) receive() (byte, []byte, error) {
	var head [5]byte
	if _, err := io.ReadFull(c.reader, head[:]); err != nil {
		return 0, nil, err
	}
	size := binary.BigEndian.Uint32(head[1:])
	if size < 4 || size-4 > maxMessage {
		return 0, nil, fmt.Errorf("the port sent a message of %d bytes", size)
	}
	payload := make([]byte, size-4)
	_, err := io.ReadFull(c.reader, payload)
	return head[0], payload, err
}

func portErrorOf(payload []byte) error {
	e := &portError{}
	for len(payload) > 1 {
		field := payload[0]
		end := strings.IndexByte(string(payload[1:]), 0)
		if end < 0 {
			break
		}
		text := string(payload[1 : 1+end])
		switch field {
		case 'C':
			e.code = text
		case 'M':
			e.message = text
		}
		payload = payload[2+end:]
	}
	return e
}

// authenticate answers SCRAM-SHA-256 (RFC 5802, 7677) up to ReadyForQuery, and checks the server's signature: a port
// that does not know the password is not the server's.
func (c *portClient) authenticate(password string) error {
	var nonce [18]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	bare := "n=,r=" + base64.RawStdEncoding.EncodeToString(nonce[:])
	var expected []byte
	for {
		kind, payload, err := c.receive()
		if err != nil {
			return err
		}
		switch kind {
		case 'E':
			return portErrorOf(payload)
		case 'R':
			if len(payload) < 4 {
				return errors.New("the port sent a short authentication message")
			}
			switch binary.BigEndian.Uint32(payload) {
			case 0: // AuthenticationOk
				if expected == nil {
					return errors.New("the port let the session in without SCRAM-SHA-256")
				}
			case 10: // AuthenticationSASL: the mechanisms
				if !strings.Contains(string(payload[4:]), "SCRAM-SHA-256\x00") {
					return errors.New("the port offers no SCRAM-SHA-256")
				}
				first := "n,," + bare
				message := append([]byte("SCRAM-SHA-256\x00"), 0, 0, 0, 0)
				binary.BigEndian.PutUint32(message[len(message)-4:], uint32(len(first)))
				if err := c.send('p', append(message, first...)); err != nil {
					return err
				}
			case 11: // AuthenticationSASLContinue: the server's first message
				serverFirst := string(payload[4:])
				fields := map[string]string{}
				for _, part := range strings.Split(serverFirst, ",") {
					if key, text, ok := strings.Cut(part, "="); ok {
						fields[key] = text
					}
				}
				salt, err := base64.StdEncoding.DecodeString(fields["s"])
				if err != nil {
					return err
				}
				iterations, err := strconv.Atoi(fields["i"])
				if err != nil || iterations < 1 || iterations > 1_000_000 {
					return errors.New("the port sent a SCRAM iteration count out of range")
				}
				if !strings.HasPrefix(fields["r"], bare[len("n=,r="):]) {
					return errors.New("the port's SCRAM nonce does not extend the client's")
				}
				salted, err := pbkdf2.Key(sha256.New, password, salt, iterations, 32)
				if err != nil {
					return err
				}
				final := "c=biws,r=" + fields["r"]
				auth := bare + "," + serverFirst + "," + final
				clientKey := scramMAC(salted, "Client Key")
				stored := sha256.Sum256(clientKey)
				signature := scramMAC(stored[:], auth)
				proof := make([]byte, len(clientKey))
				for i := range proof {
					proof[i] = clientKey[i] ^ signature[i]
				}
				expected = scramMAC(scramMAC(salted, "Server Key"), auth)
				if err := c.send('p', []byte(final+",p="+base64.StdEncoding.EncodeToString(proof))); err != nil {
					return err
				}
			case 12: // AuthenticationSASLFinal: the server's signature
				verifier, ok := strings.CutPrefix(string(payload[4:]), "v=")
				given, err := base64.StdEncoding.DecodeString(verifier)
				if !ok || err != nil || expected == nil || !hmac.Equal(given, expected) {
					return errors.New("the port's SCRAM signature is not the server's")
				}
			default:
				return fmt.Errorf("the port asks authentication %d, not SCRAM-SHA-256", binary.BigEndian.Uint32(payload))
			}
		case 'Z':
			return nil
		}
	}
}

func scramMAC(key []byte, text string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(text))
	return h.Sum(nil)
}

// exec runs one statement with the simple query protocol, up to ReadyForQuery: its first error, nil when it ran.
func (c *portClient) exec(statement string) error {
	if err := c.send('Q', append([]byte(statement), 0)); err != nil {
		return err
	}
	var failure error
	for {
		kind, payload, err := c.receive()
		if err != nil {
			return err
		}
		switch kind {
		case 'E':
			if failure == nil {
				failure = portErrorOf(payload)
			}
		case 'Z':
			return failure
		}
	}
}
