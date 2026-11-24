package host_test

import (
	"bufio"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// pgClient is the least of a PostgreSQL client the tests of the database port need, written here so engine2 keeps
// no driver: the startup, SCRAM-SHA-256 (SCRAM-SHA-256-PLUS over TLS when the server offers it, as libpq), and the
// simple query protocol, each value read as text.
type pgClient struct {
	conn   net.Conn
	reader *bufio.Reader
	// binding is tls-server-end-point of the certificate the client was served, nil without TLS
	binding []byte
	// mechanism is the SASL mechanism the client authenticated with
	mechanism string
}

// serverEndPoint is the hash of the server's certificate SCRAM-SHA-256-PLUS binds to (RFC 5929 section 4.1): by the
// hash of its signature, SHA-256 for SHA-1; nil for a signature without a hash.
func serverEndPoint(certificate *x509.Certificate) []byte {
	var sum hash.Hash
	switch certificate.SignatureAlgorithm {
	case x509.SHA1WithRSA, x509.ECDSAWithSHA1, x509.SHA256WithRSA, x509.SHA256WithRSAPSS, x509.ECDSAWithSHA256:
		sum = sha256.New()
	case x509.SHA384WithRSA, x509.SHA384WithRSAPSS, x509.ECDSAWithSHA384:
		sum = sha512.New384()
	case x509.SHA512WithRSA, x509.SHA512WithRSAPSS, x509.ECDSAWithSHA512:
		sum = sha512.New()
	default:
		return nil
	}
	sum.Write(certificate.Raw)
	return sum.Sum(nil)
}

// pgError is an ErrorResponse: its SQLSTATE and message.
type pgError struct{ code, message string }

func (e *pgError) Error() string { return e.code + ": " + e.message }

func pgConnect(address, user, password, database string) (*pgClient, error) {
	return pgConnectTLS(address, nil, user, password, database)
}

// pgConnectTLS is pgConnect through TLS when secure is not nil: an SSLRequest, the server's S, the handshake with
// secure (the name the certificate must be of, the authorities trusted), then the startup inside it.
func pgConnectTLS(address string, secure *tls.Config, user, password, database string) (*pgClient, error) {
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	if secure != nil {
		request := []byte{0, 0, 0, 8, 0x04, 0xd2, 0x16, 0x2f} // 80877103
		if _, err := conn.Write(request); err != nil {
			conn.Close()
			return nil, err
		}
		var answer [1]byte
		if _, err := io.ReadFull(conn, answer[:]); err != nil {
			conn.Close()
			return nil, err
		}
		if answer[0] != 'S' {
			conn.Close()
			return nil, fmt.Errorf("the server answered the SSLRequest %q: no TLS", answer[0])
		}
		encrypted := tls.Client(conn, secure)
		if err := encrypted.Handshake(); err != nil {
			conn.Close()
			return nil, err
		}
		conn = encrypted
	}
	c := &pgClient{conn: conn, reader: bufio.NewReader(conn)}
	if encrypted, ok := conn.(*tls.Conn); ok {
		c.binding = serverEndPoint(encrypted.ConnectionState().PeerCertificates[0])
	}
	startup := []byte{0, 0, 0, 0, 0, 3, 0, 0}
	for _, pair := range [][2]string{{"user", user}, {"database", database}} {
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

func (c *pgClient) Close() { _ = c.conn.Close() }

func (c *pgClient) send(kind byte, payload []byte) error {
	message := make([]byte, 5, 5+len(payload))
	message[0] = kind
	binary.BigEndian.PutUint32(message[1:], uint32(4+len(payload)))
	_, err := c.conn.Write(append(message, payload...))
	return err
}

func (c *pgClient) receive() (byte, []byte, error) {
	var head [5]byte
	if _, err := io.ReadFull(c.reader, head[:]); err != nil {
		return 0, nil, err
	}
	payload := make([]byte, binary.BigEndian.Uint32(head[1:])-4)
	_, err := io.ReadFull(c.reader, payload)
	return head[0], payload, err
}

func readError(payload []byte) error {
	e := &pgError{}
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

// authenticate answers SCRAM-SHA-256 (RFC 5802, 7677) up to ReadyForQuery.
func (c *pgClient) authenticate(password string) error {
	var nonce [18]byte
	_, _ = rand.Read(nonce[:])
	clientNonce := base64.RawStdEncoding.EncodeToString(nonce[:])
	bare := "n=,r=" + clientNonce
	var serverFirst, header string
	var salted []byte
	var finalWithoutProof string
	for {
		kind, payload, err := c.receive()
		if err != nil {
			return err
		}
		switch kind {
		case 'E':
			return readError(payload)
		case 'R':
			switch binary.BigEndian.Uint32(payload) {
			case 0: // ok
			case 10: // SASL: the mechanisms
				// the gs2 header as libpq writes it: p= with PLUS, y over TLS without it, n otherwise
				header, c.mechanism = "n,,", "SCRAM-SHA-256"
				if c.binding != nil {
					header = "y,,"
					for _, name := range strings.Split(string(payload[4:]), string(rune(0))) {
						if name == "SCRAM-SHA-256-PLUS" {
							header, c.mechanism = "p=tls-server-end-point,,", name
						}
					}
				}
				first := header + bare
				message := append(append([]byte(c.mechanism), 0), 0, 0, 0, 0)
				binary.BigEndian.PutUint32(message[len(message)-4:], uint32(len(first)))
				if err := c.send('p', append(message, first...)); err != nil {
					return err
				}
			case 11: // SASL continue: the server's first message
				serverFirst = string(payload[4:])
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
				if err != nil {
					return err
				}
				if salted, err = pbkdf2.Key(sha256.New, password, salt, iterations, 32); err != nil {
					return err
				}
				binding := []byte(header)
				if c.mechanism == "SCRAM-SHA-256-PLUS" {
					binding = append(binding, c.binding...)
				}
				finalWithoutProof = "c=" + base64.StdEncoding.EncodeToString(binding) + ",r=" + fields["r"]
				clientKey := mac(salted, "Client Key")
				stored := sha256.Sum256(clientKey)
				signature := mac(stored[:], bare+","+serverFirst+","+finalWithoutProof)
				proof := make([]byte, len(clientKey))
				for i := range proof {
					proof[i] = clientKey[i] ^ signature[i]
				}
				if err := c.send('p', []byte(finalWithoutProof+",p="+base64.StdEncoding.EncodeToString(proof))); err != nil {
					return err
				}
			case 12: // SASL final: the server's signature
			default:
				return fmt.Errorf("authentication %d is not SCRAM-SHA-256", binary.BigEndian.Uint32(payload))
			}
		case 'Z':
			return nil
		}
	}
}

func mac(key []byte, text string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(text))
	return h.Sum(nil)
}

// query runs one simple query: its rows, each value its text (nil for NULL), and the last command's tag.
func (c *pgClient) query(text string) ([][]any, string, error) {
	if err := c.send('Q', append([]byte(text), 0)); err != nil {
		return nil, "", err
	}
	return c.results()
}

// queryWith runs one statement with parameters ($1, $2, …), each sent as text, through the extended protocol.
func (c *pgClient) queryWith(text string, arguments ...string) ([][]any, string, error) {
	parse := append(append([]byte{0}, text...), 0, 0, 0)
	bind := []byte{0, 0, 0, 0, byte(len(arguments) >> 8), byte(len(arguments))}
	for _, argument := range arguments {
		size := make([]byte, 4)
		binary.BigEndian.PutUint32(size, uint32(len(argument)))
		bind = append(append(bind, size...), argument...)
	}
	bind = append(bind, 0, 0)
	for _, message := range []struct {
		kind    byte
		payload []byte
	}{{'P', parse}, {'B', bind}, {'E', []byte{0, 0, 0, 0, 0}}, {'S', nil}} {
		if err := c.send(message.kind, message.payload); err != nil {
			return nil, "", err
		}
	}
	return c.results()
}

// results reads up to ReadyForQuery: the rows, the last tag, the first error.
func (c *pgClient) results() ([][]any, string, error) {
	var rows [][]any
	var tag string
	var failure error
	for {
		kind, payload, err := c.receive()
		if err != nil {
			return nil, "", err
		}
		switch kind {
		case 'D':
			count := int(binary.BigEndian.Uint16(payload))
			payload = payload[2:]
			row := make([]any, count)
			for i := range row {
				size := int32(binary.BigEndian.Uint32(payload))
				payload = payload[4:]
				if size >= 0 {
					row[i] = string(payload[:size])
					payload = payload[size:]
				}
			}
			rows = append(rows, row)
		case 'C':
			tag = strings.TrimRight(string(payload), "\x00")
		case 'E':
			if failure == nil {
				failure = readError(payload)
			}
		case 'Z':
			return rows, tag, failure
		}
	}
}
