// Package unpack restores a codepack envelope to a directory tree and
// verifies every restored file against its stored SHA-256. A pack is not
// considered restored until every hash matches; any mismatch fails loudly
// with the file named.
package unpack

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/evan-william/codepack-lz/internal/format"
	"github.com/evan-william/codepack-lz/internal/version"
	"github.com/klauspost/compress/zstd"
)

// ErrLegacyFormat identifies envelopes from the pre-release v0.1 prototype,
// which used a different payload layout and cannot be read by this version.
var ErrLegacyFormat = errors.New("this is a legacy CodePack-LZ v0.1 prototype envelope; repack the source with the current tool")

// Header is the plaintext metadata block that precedes the payload fence.
// It is readable without decoding the payload -- that is the point.
type Header struct {
	FormatVersion int
	ToolVersion   string
	Encoding      string
	Codec         string
	Encryption    string
	Nonce         string
	Created       string
	Root          string
	Files         int
	Skipped       int
	BytesRaw      int64
	BytesPacked   int64
	HashAlgo      string
	SecretScan    string
	Warning       string
}

// Summary reports what Restore did.
type Summary struct {
	Root     string
	Files    int
	Bytes    int64
	Verified int
	DryRun   bool
}

// RestoreOptions configures RestoreWithOptions.
type RestoreOptions struct {
	DryRun        bool
	EncryptionKey []byte
}

// ReadHeader parses the plaintext header, consuming the reader only up to
// (and including) the BEGIN marker line. The payload is not decoded.
func ReadHeader(br *bufio.Reader) (*Header, error) {
	first, err := readLine(br)
	if err != nil {
		return nil, fmt.Errorf("read magic line: %w", err)
	}
	if strings.HasPrefix(first, "CODEPACK-LZ v0.1") {
		return nil, ErrLegacyFormat
	}
	if first != format.MagicLine {
		return nil, fmt.Errorf("not a codepack envelope: first line is %q, want %q", truncate(first, 40), format.MagicLine)
	}

	h := &Header{}
	for {
		line, err := readLine(br)
		if err != nil {
			return nil, fmt.Errorf("read header: %w (missing %q marker?)", err, format.BeginMarker)
		}
		if line == format.BeginMarker {
			break
		}
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, ": ")
		if !found {
			return nil, fmt.Errorf("malformed header line %q", truncate(line, 60))
		}
		switch key {
		case "Format-Version":
			h.FormatVersion, _ = strconv.Atoi(value)
		case "Tool-Version":
			h.ToolVersion = value
		case "Encoding":
			h.Encoding = value
		case "Codec":
			h.Codec = value
		case "Encryption":
			h.Encryption = value
		case "Nonce":
			h.Nonce = value
		case "Created":
			h.Created = value
		case "Root":
			h.Root = value
		case "Files":
			h.Files, _ = strconv.Atoi(value)
		case "Skipped":
			h.Skipped, _ = strconv.Atoi(value)
		case "Bytes-Raw":
			h.BytesRaw, _ = strconv.ParseInt(value, 10, 64)
		case "Bytes-Packed":
			h.BytesPacked, _ = strconv.ParseInt(value, 10, 64)
		case "Hash-Algo":
			h.HashAlgo = value
		case "Secret-Scan":
			h.SecretScan = value
		case "Warning":
			h.Warning = value
		default:
			// Unknown keys from newer minor revisions are tolerated.
		}
	}

	if h.FormatVersion == 0 {
		return nil, errors.New("header missing Format-Version")
	}
	if h.FormatVersion > version.FormatVersion {
		return nil, fmt.Errorf("envelope format v%d is newer than this tool supports (v%d); upgrade codepack-lz", h.FormatVersion, version.FormatVersion)
	}
	if h.Encoding != "base64" {
		return nil, fmt.Errorf("unsupported encoding %q", h.Encoding)
	}
	if h.Codec != format.CodecGzip && h.Codec != format.CodecZstd {
		return nil, fmt.Errorf("unsupported codec %q", h.Codec)
	}
	if h.Encryption == "" {
		h.Encryption = format.EncryptionNone
	}
	switch h.Encryption {
	case format.EncryptionNone:
	case format.EncryptionAES256GCM:
		if h.Nonce == "" {
			return nil, errors.New("encrypted payload is missing Nonce header")
		}
		if _, err := base64.StdEncoding.DecodeString(h.Nonce); err != nil {
			return nil, fmt.Errorf("invalid Nonce header: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported encryption %q", h.Encryption)
	}
	return h, nil
}

// Restore reads an envelope from r and writes the tree under outDir,
// verifying every file hash. With dryRun it decodes and verifies everything
// but writes nothing. Restore never overwrites an existing file.
func Restore(r io.Reader, outDir string, dryRun bool) (*Summary, error) {
	return RestoreWithOptions(r, outDir, RestoreOptions{DryRun: dryRun})
}

// RestoreWithOptions reads an envelope from r and writes the tree under outDir
// with optional decryption support.
func RestoreWithOptions(r io.Reader, outDir string, opts RestoreOptions) (*Summary, error) {
	br := bufio.NewReaderSize(r, 64*1024)
	h, err := ReadHeader(br)
	if err != nil {
		return nil, err
	}

	zr, err := openPayload(br, h, opts.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("open payload: %w", err)
	}
	defer func() { _ = zr.Close() }()

	dec := json.NewDecoder(zr)

	// First NDJSON value must be the manifest.
	var manifest struct {
		Type     string `json:"type"`
		Root     string `json:"root"`
		Files    int    `json:"files"`
		HashAlgo string `json:"hash_algo"`
	}
	if err := dec.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	if manifest.Type != "manifest" {
		return nil, fmt.Errorf("payload does not start with a manifest (got type %q)", manifest.Type)
	}
	if manifest.HashAlgo != "sha256" {
		return nil, fmt.Errorf("unsupported hash algorithm %q", manifest.HashAlgo)
	}

	absOut, err := filepath.Abs(outDir)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", outDir, err)
	}

	sum := &Summary{Root: manifest.Root, DryRun: opts.DryRun}
	seen := make(map[string]string, manifest.Files) // path -> sha256

	for {
		var f struct {
			Type    string  `json:"type"`
			Path    string  `json:"path"`
			Size    int64   `json:"size"`
			SHA256  string  `json:"sha256"`
			Content *string `json:"content"`
			DupOf   string  `json:"dup_of"`
		}
		if err := dec.Decode(&f); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode payload after %d file(s): %w", sum.Files, err)
		}
		if f.Type != "file" {
			return nil, fmt.Errorf("unexpected payload entry type %q", f.Type)
		}
		if err := safeRelPath(f.Path); err != nil {
			return nil, fmt.Errorf("unsafe path in pack: %w", err)
		}
		if _, dup := seen[f.Path]; dup {
			return nil, fmt.Errorf("duplicate path in pack: %s", f.Path)
		}

		var content []byte
		switch {
		case f.DupOf != "":
			refSHA, ok := seen[f.DupOf]
			if !ok {
				return nil, fmt.Errorf("%s: dup_of references %q, which does not precede it", f.Path, f.DupOf)
			}
			if refSHA != f.SHA256 {
				return nil, fmt.Errorf("%s: dup_of hash mismatch with %s", f.Path, f.DupOf)
			}
			if !opts.DryRun {
				content, err = os.ReadFile(filepath.Join(absOut, filepath.FromSlash(f.DupOf)))
				if err != nil {
					return nil, fmt.Errorf("%s: read canonical copy: %w", f.Path, err)
				}
			}
		case f.Content != nil:
			content = []byte(*f.Content)
		default:
			return nil, fmt.Errorf("%s: entry has neither content nor dup_of", f.Path)
		}

		// Verify before write: the stored hash must match the stored bytes.
		if f.DupOf == "" || !opts.DryRun {
			got := sha256.Sum256(content)
			if hex.EncodeToString(got[:]) != f.SHA256 {
				return nil, fmt.Errorf("hash mismatch for %s: pack is corrupted or was modified", f.Path)
			}
			sum.Verified++
		}

		if !opts.DryRun {
			target := filepath.Join(absOut, filepath.FromSlash(f.Path))
			if _, err := os.Lstat(target); err == nil {
				return nil, fmt.Errorf("refusing to overwrite existing file: %s", target)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return nil, fmt.Errorf("create directory for %s: %w", f.Path, err)
			}
			if err := os.WriteFile(target, content, 0o644); err != nil {
				return nil, fmt.Errorf("write %s: %w", f.Path, err)
			}
		}

		seen[f.Path] = f.SHA256
		sum.Files++
		sum.Bytes += f.Size
	}

	if sum.Files != manifest.Files {
		return nil, fmt.Errorf("manifest declares %d files but payload contains %d", manifest.Files, sum.Files)
	}
	if h.Files != manifest.Files {
		return nil, fmt.Errorf("plaintext header declares %d files but manifest declares %d: header was edited", h.Files, manifest.Files)
	}
	return sum, nil
}

func openPayload(br *bufio.Reader, h *Header, key []byte) (io.ReadCloser, error) {
	payload := base64.NewDecoder(base64.StdEncoding, &payloadReader{br: br})
	compressed := payload
	if h.Encryption == format.EncryptionAES256GCM {
		if len(key) != 32 {
			return nil, fmt.Errorf("AES-256-GCM envelope requires a 32-byte key")
		}
		ciphertext, err := io.ReadAll(payload)
		if err != nil {
			return nil, err
		}
		nonce, err := base64.StdEncoding.DecodeString(h.Nonce)
		if err != nil {
			return nil, err
		}
		plaintext, err := decryptAESGCM(key, nonce, ciphertext)
		if err != nil {
			return nil, err
		}
		compressed = bytes.NewReader(plaintext)
	}

	switch h.Codec {
	case format.CodecGzip:
		return gzip.NewReader(compressed)
	case format.CodecZstd:
		zr, err := zstd.NewReader(compressed)
		if err != nil {
			return nil, err
		}
		return zstdReadCloser{Decoder: zr}, nil
	default:
		return nil, fmt.Errorf("unsupported codec %q", h.Codec)
	}
}

type zstdReadCloser struct {
	*zstd.Decoder
}

func (z zstdReadCloser) Close() error {
	z.Decoder.Close()
	return nil
}

func decryptAESGCM(key, nonce, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// payloadReader feeds base64 payload lines to the decoder, skipping line
// breaks and stopping cleanly at the END marker.
type payloadReader struct {
	br   *bufio.Reader
	done bool
	rest []byte
}

func (p *payloadReader) Read(out []byte) (int, error) {
	for len(p.rest) == 0 {
		if p.done {
			return 0, io.EOF
		}
		line, err := readLine(p.br)
		if err != nil {
			if err == io.EOF {
				return 0, fmt.Errorf("payload ended without %q marker", format.EndMarker)
			}
			return 0, err
		}
		if line == format.EndMarker {
			p.done = true
			return 0, io.EOF
		}
		p.rest = []byte(strings.TrimSpace(line))
	}
	n := copy(out, p.rest)
	p.rest = p.rest[n:]
	return n, nil
}

// safeRelPath rejects anything that could escape the output directory:
// absolute paths, drive letters, "..", backslashes, empty segments, NULs.
func safeRelPath(p string) error {
	switch {
	case p == "":
		return errors.New("empty path")
	case strings.ContainsAny(p, "\\\x00"):
		return fmt.Errorf("%s: backslash or NUL in path", strconv.Quote(p))
	case strings.HasPrefix(p, "/"):
		return fmt.Errorf("%s: absolute path", p)
	case len(p) >= 2 && p[1] == ':':
		return fmt.Errorf("%s: drive letter", p)
	}
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "", ".", "..":
			return fmt.Errorf("%s: illegal path segment %q", p, seg)
		}
	}
	return nil
}

// readLine reads one \n-terminated line, tolerating \r\n, without any length
// limit surprises (file content is inside the base64 payload, so header and
// payload lines are always short).
func readLine(br *bufio.Reader) (string, error) {
	line, err := br.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
