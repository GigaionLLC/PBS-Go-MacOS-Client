// Package protocol implements the PBS wire protocol: fingerprint-pinned TLS,
// API-token auth, the plain JSON REST calls (version, snapshot listing), and
// the HTTP/2-upgraded writer (backup) and reader (restore) sessions.
//
// Confidence map (see docs/DESIGN.md §4):
//   - TLS fingerprint pinning + auth ....... high (offline-testable)
//   - version / snapshot listing ........... high (ordinary REST; validate first)
//   - HTTP/2 upgrade handshake ............. medium (mechanism is standard)
//   - writer/reader endpoint params ........ INFERRED — marked "VALIDATE" inline
//
// Spots marked VALIDATE are best-effort from the protocol docs and need a live
// PBS to confirm exact endpoint paths / query parameters. They are written so
// validation is a matter of tweaking constants, not rewriting structure.
package protocol

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/repo"
)

// Credentials authenticate a session.
type Credentials struct {
	// APIToken is "USER@REALM!TOKENID:SECRET".
	APIToken string
	// Fingerprint is the expected server cert SHA-256 (hex, ':'-separated ok).
	// Empty means rely on the system trust store.
	Fingerprint string
}

// Client is a connection factory for a PBS datastore.
type Client struct {
	repo  *repo.Repository
	creds Credentials

	// Namespace selects a datastore namespace ("" = root). Sent as the "ns"
	// query parameter on session upgrades and listings.
	Namespace string

	rest *http.Client // lazily built, pinned-TLS REST client
}

// Dial builds a client for the given repository and credentials.
func Dial(r *repo.Repository, creds Credentials) (*Client, error) {
	if r == nil {
		return nil, errors.New("nil repository")
	}
	if r.Host == "" {
		return nil, errors.New("repository has no host to connect to")
	}
	return &Client{repo: r, creds: creds}, nil
}

// baseURL is https://host:port.
func (c *Client) baseURL() string {
	return fmt.Sprintf("https://%s:%d", c.repo.Host, c.repo.Port)
}

func (c *Client) addr() string {
	return fmt.Sprintf("%s:%d", c.repo.Host, c.repo.Port)
}

// parseFingerprint accepts "AA:BB:.." or "aabb.." hex and returns 32 bytes, or
// nil if the input is empty (meaning: use the system trust store).
func parseFingerprint(s string) ([]byte, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ":", "")
	if s == "" {
		return nil, nil
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("fingerprint is not valid hex: %w", err)
	}
	if len(b) != sha256.Size {
		return nil, fmt.Errorf("fingerprint must be %d bytes (SHA-256), got %d", sha256.Size, len(b))
	}
	return b, nil
}

// tlsConfig builds a TLS config that pins the server cert by SHA-256 when a
// fingerprint is configured (the norm for self-signed PBS certs). alpn selects
// the ALPN protocols to advertise; for the HTTP/2 upgrade we must request
// "http/1.1" so the initial upgrade request is spoken over HTTP/1.1.
func (c *Client) tlsConfig(alpn ...string) (*tls.Config, error) {
	fp, err := parseFingerprint(c.creds.Fingerprint)
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{
		ServerName: c.repo.Host,
		MinVersion: tls.VersionTLS12,
		NextProtos: alpn,
	}
	if fp != nil {
		cfg.InsecureSkipVerify = true // we verify by pinned fingerprint instead
		cfg.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("server presented no certificate")
			}
			sum := sha256.Sum256(cs.PeerCertificates[0].Raw)
			if !bytes.Equal(sum[:], fp) {
				return fmt.Errorf("server cert fingerprint %s does not match pinned value",
					hex.EncodeToString(sum[:]))
			}
			return nil
		}
	}
	return cfg, nil
}

// restClient returns a cached HTTP/1.1+2 client with pinned TLS for plain REST.
func (c *Client) restClient() (*http.Client, error) {
	if c.rest != nil {
		return c.rest, nil
	}
	cfg, err := c.tlsConfig()
	if err != nil {
		return nil, err
	}
	c.rest = &http.Client{
		Timeout:   60 * time.Second,
		Transport: &http.Transport{TLSClientConfig: cfg},
	}
	return c.rest, nil
}

// setAuth adds the API-token Authorization header if configured.
func (c *Client) setAuth(req *http.Request) {
	if c.creds.APIToken != "" {
		// PBS API-token scheme. If a server rejects this, the only likely tweak
		// is the "PBSAPIToken=" prefix or ':' vs '=' before the secret.
		req.Header.Set("Authorization", "PBSAPIToken="+c.creds.APIToken)
	}
}

// envelope is the PBS JSON response wrapper: {"data": ...}.
type envelope struct {
	Data json.RawMessage `json:"data"`
}

func snippet(b []byte) string {
	const max = 300
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

// getJSON performs GET path and decodes the {"data": ...} payload into out.
func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	cl, err := c.restClient()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL()+path, nil)
	if err != nil {
		return err
	}
	c.setAuth(req)
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("GET %s: %s: %s", path, resp.Status, snippet(body))
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	if out != nil && len(env.Data) > 0 {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

// Version is the server version info from GET /api2/json/version.
type Version struct {
	Version string `json:"version"`
	Release string `json:"release"`
	RepoID  string `json:"repoid"`
}

// GetVersion fetches the server version. This is the cheapest end-to-end check
// of connectivity + TLS pinning + auth — validate this first.
func (c *Client) GetVersion(ctx context.Context) (*Version, error) {
	var v Version
	if err := c.getJSON(ctx, "/api2/json/version", &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// Snapshot identifies a backup snapshot within a datastore.
type Snapshot struct {
	Type    string `json:"backup-type"` // "host", "vm", "ct"
	ID      string `json:"backup-id"`
	Time    int64  `json:"backup-time"` // unix seconds
	Comment string `json:"comment,omitempty"`
	Size    int64  `json:"size,omitempty"`
}

// ListSnapshots returns the snapshots in the datastore via
// GET /api2/json/admin/datastore/{store}/snapshots.
func (c *Client) ListSnapshots(ctx context.Context) ([]Snapshot, error) {
	path := "/api2/json/admin/datastore/" + url.PathEscape(c.repo.Datastore) + "/snapshots"
	var snaps []Snapshot
	if err := c.getJSON(ctx, path, &snaps); err != nil {
		return nil, err
	}
	return snaps, nil
}

// hexDigest renders a 32-byte digest as lowercase hex.
func hexDigest(d [32]byte) string { return hex.EncodeToString(d[:]) }

// itoa is a small helper for building query parameters.
func itoa[T ~int | ~int64 | ~uint32 | ~uint64](v T) string {
	return strconv.FormatUint(uint64(v), 10)
}
