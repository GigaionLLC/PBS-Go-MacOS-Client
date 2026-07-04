package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
)

// BackupWriter is an open backup (writer) session against a new snapshot. The
// endpoint paths and parameters below are taken from the official PBS source
// (src/api2/backup/mod.rs and upload_chunk.rs; see docs/DESIGN.md §4.1). Where a
// detail still needs a live server to confirm end-to-end, it is marked VALIDATE.
type BackupWriter struct {
	c   *Client
	s   *session
	ctx context.Context
}

// BeginBackup starts a writer session for a new snapshot by upgrading
// GET /api2/json/backup to the writer protocol. The upgrade requires the "store"
// query parameter (the datastore); "ns" selects a namespace (empty = root);
// "debug"/"benchmark" are optional.
func (c *Client) BeginBackup(ctx context.Context, snap Snapshot) (*BackupWriter, error) {
	q := url.Values{}
	q.Set("store", c.repo.Datastore)
	q.Set("backup-type", snap.Type)
	q.Set("backup-id", snap.ID)
	q.Set("backup-time", itoa(snap.Time))
	if c.Namespace != "" {
		q.Set("ns", c.Namespace)
	}
	sess, err := c.dialUpgrade(ctx, "/api2/json/backup", writerProtocol, q)
	if err != nil {
		return nil, err
	}
	return &BackupWriter{c: c, s: sess, ctx: ctx}, nil
}

// readData reads a response, checks status, and returns the {"data": ...} bytes.
func readData(r *responseCloser) (json.RawMessage, error) {
	defer r.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if r.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%s %s: %s: %s", r.method, r.path, r.Status, snippet(body))
	}
	if len(body) == 0 {
		return nil, nil
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		// Some endpoints return a bare value, not an envelope.
		return json.RawMessage(body), nil
	}
	return env.Data, nil
}

// CreateDynamicIndex opens a dynamically sized index for a file archive
// (e.g. "root.pxar.didx") and returns the server-assigned writer id (wid).
// POST dynamic_index with query "archive-name"; the response is a bare integer.
func (w *BackupWriter) CreateDynamicIndex(archiveName string) (int, error) {
	q := url.Values{}
	q.Set("archive-name", archiveName)
	resp, err := w.request("POST", "/dynamic_index", q, nil, "")
	if err != nil {
		return 0, err
	}
	data, err := readData(resp)
	if err != nil {
		return 0, err
	}
	var wid int
	if err := json.Unmarshal(data, &wid); err != nil {
		return 0, fmt.Errorf("parse writer id from %q: %w", string(data), err)
	}
	return wid, nil
}

// UploadChunk uploads one encoded chunk (a PBS DataBlob) for the given writer.
// rawSize is the plaintext size; blob is the encoded/encrypted chunk bytes.
// POST dynamic_chunk with query wid/digest/size/encoded-size; body is the blob.
func (w *BackupWriter) UploadChunk(wid int, digest [32]byte, rawSize uint32, blob []byte) error {
	q := url.Values{}
	q.Set("wid", itoa(wid))
	q.Set("digest", hexDigest(digest))
	q.Set("size", itoa(rawSize))
	q.Set("encoded-size", itoa(uint32(len(blob))))
	resp, err := w.requestBytes("POST", "/dynamic_chunk", q, blob, "application/octet-stream")
	if err != nil {
		return err
	}
	_, err = readData(resp)
	return err
}

// AppendChunks records a batch of chunk digests + offsets into the open index.
// PUT dynamic_index with JSON {"wid", "digest-list", "offset-list"}.
func (w *BackupWriter) AppendChunks(wid int, digests [][32]byte, offsets []uint64) error {
	dl := make([]string, len(digests))
	for i, d := range digests {
		dl[i] = hexDigest(d)
	}
	payload, _ := json.Marshal(map[string]any{
		"wid":         wid,
		"digest-list": dl,
		"offset-list": offsets,
	})
	resp, err := w.requestBytes("PUT", "/dynamic_index", nil, payload, "application/json")
	if err != nil {
		return err
	}
	_, err = readData(resp)
	return err
}

// CloseDynamicIndex finalizes an index.
// POST dynamic_close with query wid/chunk-count/size/csum (csum hex-encoded).
func (w *BackupWriter) CloseDynamicIndex(wid int, chunkCount, totalSize uint64, csum [32]byte) error {
	q := url.Values{}
	q.Set("wid", itoa(wid))
	q.Set("chunk-count", itoa(chunkCount))
	q.Set("size", itoa(totalSize))
	q.Set("csum", hexDigest(csum))
	resp, err := w.request("POST", "/dynamic_close", q, nil, "")
	if err != nil {
		return err
	}
	_, err = readData(resp)
	return err
}

// UploadBlob uploads a named blob (e.g. the "index.json.blob" manifest or the
// backup catalog). POST blob with query "file-name" and "encoded-size".
//
// VALIDATE: the blob body must itself be a PBS DataBlob (magic + optional
// crc/compression/encryption framing), not raw bytes — see docs/DESIGN.md §5.
func (w *BackupWriter) UploadBlob(name string, blob []byte) error {
	q := url.Values{}
	q.Set("file-name", name)
	q.Set("encoded-size", itoa(uint32(len(blob))))
	resp, err := w.requestBytes("POST", "/blob", q, blob, "application/octet-stream")
	if err != nil {
		return err
	}
	_, err = readData(resp)
	return err
}

// Finish commits the backup snapshot. POST finish (no parameters).
func (w *BackupWriter) Finish() error {
	resp, err := w.request("POST", "/finish", nil, nil, "")
	if err != nil {
		return err
	}
	_, err = readData(resp)
	return err
}

// Close tears down the session.
func (w *BackupWriter) Close() error { return w.s.close() }

// responseCloser bundles an *http.Response with the method/path for errors.
type responseCloser struct {
	Body       io.ReadCloser
	StatusCode int
	Status     string
	method     string
	path       string
}

func (w *BackupWriter) request(method, path string, q url.Values, body io.Reader, ct string) (*responseCloser, error) {
	resp, err := w.s.do(w.ctx, method, path, q, body, ct)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	return &responseCloser{Body: resp.Body, StatusCode: resp.StatusCode, Status: resp.Status, method: method, path: path}, nil
}

func (w *BackupWriter) requestBytes(method, path string, q url.Values, body []byte, ct string) (*responseCloser, error) {
	return w.request(method, path, q, bytesReader(body), ct)
}
