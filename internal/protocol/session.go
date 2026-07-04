package protocol

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"

	"golang.org/x/net/http2"
)

// Protocol names for the two upgraded session types.
const (
	writerProtocol = "proxmox-backup-protocol-v1"
	readerProtocol = "proxmox-backup-reader-protocol-v1"
)

// session is an HTTP/2 connection established by upgrading an HTTPS request,
// as PBS's backup and reader endpoints require.
type session struct {
	conn net.Conn
	cc   *http2.ClientConn
	host string // host:port, for building absolute request URLs
}

// bufConn presents a net.Conn whose reads come from a bufio.Reader (so any
// bytes buffered while reading the upgrade response are not lost) while writes
// go straight to the underlying connection.
type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufConn) Read(p []byte) (int, error) { return b.r.Read(p) }

// dialUpgrade opens a TLS connection, performs an HTTP/1.1 Upgrade to the given
// PBS sub-protocol (which is HTTP/2 on the wire), and returns a session that
// speaks HTTP/2 over the upgraded socket.
func (c *Client) dialUpgrade(ctx context.Context, apiPath, proto string, q url.Values) (*session, error) {
	cfg, err := c.tlsConfig("http/1.1") // force HTTP/1.1 ALPN for the upgrade request
	if err != nil {
		return nil, err
	}
	d := &tls.Dialer{Config: cfg}
	raw, err := d.DialContext(ctx, "tcp", c.addr())
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", c.addr(), err)
	}

	target := apiPath
	if enc := q.Encode(); enc != "" {
		target += "?" + enc
	}
	// Hand-write the HTTP/1.1 upgrade request over the TLS socket.
	fmt.Fprintf(raw, "GET %s HTTP/1.1\r\n", target)
	fmt.Fprintf(raw, "Host: %s\r\n", c.repo.Host)
	if c.creds.APIToken != "" {
		fmt.Fprintf(raw, "Authorization: PBSAPIToken=%s\r\n", c.creds.APIToken)
	}
	fmt.Fprintf(raw, "Connection: Upgrade\r\n")
	fmt.Fprintf(raw, "Upgrade: %s\r\n", proto)
	fmt.Fprintf(raw, "\r\n")

	br := bufio.NewReader(raw)
	dummyReq, _ := http.NewRequest(http.MethodGet, target, nil)
	resp, err := http.ReadResponse(br, dummyReq)
	if err != nil {
		raw.Close()
		return nil, fmt.Errorf("read upgrade response: %w", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		raw.Close()
		return nil, fmt.Errorf("upgrade to %s failed: %s: %s", proto, resp.Status, snippet(body))
	}

	// The socket now carries HTTP/2. Wrap it so buffered bytes survive, then
	// start an HTTP/2 client connection (this sends the h2 client preface).
	conn := &bufConn{Conn: raw, r: br}
	tr := &http2.Transport{}
	cc, err := tr.NewClientConn(conn)
	if err != nil {
		raw.Close()
		return nil, fmt.Errorf("start HTTP/2 over upgraded conn: %w", err)
	}
	return &session{conn: raw, cc: cc, host: c.addr()}, nil
}

// do issues an HTTP/2 request over the upgraded session. body may be nil.
func (s *session) do(ctx context.Context, method, path string, q url.Values, body io.Reader, contentType string) (*http.Response, error) {
	u := "https://" + s.host + path
	if enc := q.Encode(); enc != "" {
		u += "?" + enc
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return s.cc.RoundTrip(req)
}

// close tears down the session.
func (s *session) close() error {
	if s.cc != nil {
		_ = s.cc.Close()
	}
	return s.conn.Close()
}
