package backup

import (
	"context"
	"fmt"

	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/datablob"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/manifest"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/protocol"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/pxar"
)

// protocolSink uploads each unique chunk to an open backup writer session.
type protocolSink struct {
	w   *protocol.BackupWriter
	wid int
}

// Put implements ChunkSink.
func (s protocolSink) Put(digest [32]byte, rawSize uint32, blob []byte) error {
	return s.w.UploadChunk(s.wid, digest, rawSize, blob)
}

// Upload performs a full backup of fs (rooted at root) into a new snapshot on
// the server. It drives the complete PBS writer sequence:
//
//	BeginBackup -> CreateDynamicIndex -> (upload chunks) -> AppendChunks ->
//	CloseDynamicIndex -> UploadBlob(manifest) -> Finish
//
// archiveName is the logical archive name (e.g. "root.pxar"); the on-server
// dynamic index is archiveName+".didx". The endpoint-level details this depends
// on are ported from the PBS source but not yet exercised against a live server
// (see docs/DESIGN.md); this is the code to validate first once a target exists.
func Upload(ctx context.Context, c *protocol.Client, snap protocol.Snapshot, archiveName string, fs pxar.Filesystem, root string, opts Options) (Result, error) {
	w, err := c.BeginBackup(ctx, snap)
	if err != nil {
		return Result{}, fmt.Errorf("begin backup: %w", err)
	}
	defer w.Close()

	didx := archiveName + ".didx"
	wid, err := w.CreateDynamicIndex(didx)
	if err != nil {
		return Result{}, fmt.Errorf("create dynamic index: %w", err)
	}

	opts.Ctime = snap.Time
	res, idx, err := Run(fs, root, protocolSink{w: w, wid: wid}, opts)
	if err != nil {
		return Result{}, err
	}

	// Append every chunk (in order, including duplicates) to the index. PBS's
	// dynamic_writer_append_chunk validates each incoming offset against its
	// running stream position *before* adding the chunk's size, so the
	// offset-list must carry each chunk's START offset (0 for the first, then the
	// previous chunk's end), not its end. The server derives the size from the
	// already-uploaded chunk and stores the end offset itself
	// (src/api2/backup/environment.rs).
	entries := idx.Entries()
	digests := make([][32]byte, len(entries))
	offsets := make([]uint64, len(entries))
	var start uint64
	for i, e := range entries {
		digests[i] = e.Digest
		offsets[i] = start
		start = e.End
	}
	if err := w.AppendChunks(wid, digests, offsets); err != nil {
		return res, fmt.Errorf("append chunks: %w", err)
	}
	if err := w.CloseDynamicIndex(wid, idx.ChunkCount(), idx.Size(), idx.Csum()); err != nil {
		return res, fmt.Errorf("close index: %w", err)
	}

	// Build and upload the manifest (index.json.blob).
	cm := manifest.CryptNone
	if opts.Crypt != nil {
		cm = manifest.CryptEncrypt
	}
	m := manifest.New(snap.Type, snap.ID, snap.Time)
	m.AddFile(manifest.FileInfo{Filename: didx, Size: idx.Size(), Csum: idx.Csum(), CryptMode: cm})

	// Encrypted backups sign the manifest with HMAC-SHA256(id_key, canonical_json)
	// and record the key fingerprint under unprotected["key-fingerprint"];
	// JSONSigned does both. Unencrypted backups need no signature.
	var mjson []byte
	if opts.Crypt != nil {
		mjson, err = m.JSONSigned(opts.Crypt)
	} else {
		mjson, err = m.JSON()
	}
	if err != nil {
		return res, fmt.Errorf("marshal manifest: %w", err)
	}
	// The manifest blob is stored unencrypted -- it is signed (above), not encrypted.
	mblob, err := datablob.Encode(mjson, nil, false)
	if err != nil {
		return res, fmt.Errorf("encode manifest blob: %w", err)
	}
	if err := w.UploadBlob(manifest.BlobName, mblob); err != nil {
		return res, fmt.Errorf("upload manifest: %w", err)
	}

	if err := w.Finish(); err != nil {
		return res, fmt.Errorf("finish backup: %w", err)
	}
	return res, nil
}
