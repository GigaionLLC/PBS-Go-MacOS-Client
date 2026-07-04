package protocol

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
)

// bytesReader returns an io.Reader for b, or nil for an empty slice.
func bytesReader(b []byte) io.Reader {
	if len(b) == 0 {
		return nil
	}
	return bytes.NewReader(b)
}

// RestoreReader is an open restore (reader) session against an existing
// snapshot. Endpoints are from the official PBS source (src/api2/reader/mod.rs;
// see docs/DESIGN.md §4.2): "download" (file-name) and "chunk" (digest).
type RestoreReader struct {
	c   *Client
	s   *session
	ctx context.Context
}

// BeginRestore starts a reader session by upgrading GET /api2/json/reader. Like
// the writer, it requires the "store" query parameter and accepts "ns".
func (c *Client) BeginRestore(ctx context.Context, snap Snapshot) (*RestoreReader, error) {
	q := url.Values{}
	q.Set("store", c.repo.Datastore)
	q.Set("backup-type", snap.Type)
	q.Set("backup-id", snap.ID)
	q.Set("backup-time", itoa(snap.Time))
	if c.Namespace != "" {
		q.Set("ns", c.Namespace)
	}
	sess, err := c.dialUpgrade(ctx, "/api2/json/reader", readerProtocol, q)
	if err != nil {
		return nil, err
	}
	return &RestoreReader{c: c, s: sess, ctx: ctx}, nil
}

// Download fetches a named file from the snapshot (e.g. the manifest or an
// index file) via the "download" endpoint with query "file-name".
func (r *RestoreReader) Download(name string) ([]byte, error) {
	q := url.Values{}
	q.Set("file-name", name)
	resp, err := r.s.do(r.ctx, "GET", "/download", q, nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("download %s: %s: %s", name, resp.Status, snippet(body))
	}
	return body, nil
}

// Chunk fetches one chunk (an encoded DataBlob) by digest via the "chunk"
// endpoint with query "digest".
func (r *RestoreReader) Chunk(digest [32]byte) ([]byte, error) {
	q := url.Values{}
	q.Set("digest", hexDigest(digest))
	resp, err := r.s.do(r.ctx, "GET", "/chunk", q, nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("chunk %s: %s: %s", hexDigest(digest), resp.Status, snippet(body))
	}
	return body, nil
}

// Close tears down the session.
func (r *RestoreReader) Close() error { return r.s.close() }
