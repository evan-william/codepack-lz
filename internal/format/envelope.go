package format

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/evan-william/codepack-lz/internal/model"
	"github.com/evan-william/codepack-lz/internal/version"
	"github.com/klauspost/compress/zstd"
)

// Envelope format constants. The full spec lives in docs/format-spec.md;
// this file is the reference implementation of the write side.
const (
	MagicLine   = "CODEPACK-LZ v1"
	BeginMarker = "--- BEGIN CODEPACK PAYLOAD ---"
	EndMarker   = "--- END CODEPACK PAYLOAD ---"

	CodecGzip = "gzip"
	CodecZstd = "zstd"

	EncryptionNone      = "none"
	EncryptionAES256GCM = "aes-256-gcm"

	// SecurityWarning appears verbatim in every envelope header. Base64 looks
	// opaque but is trivially decodable; nobody may mistake it for encryption.
	SecurityWarning  = "Base64 is encoding, not encryption. This file exposes every secret it contains."
	EncryptedWarning = "Payload is AES-256-GCM encrypted. Keep the key private; secret scanning still matters."

	base64LineWidth = 76 // RFC 2045 wrapping
	aes256KeyBytes  = 32
	aesGCMNonceSize = 12
)

// Envelope renders the lossless transport format: an NDJSON payload
// (manifest line, then one line per file, path-ascending) compressed with
// the codec, optionally encrypted, and base64-encoded between plaintext
// markers. The header before the fence is readable without decoding anything.
type Envelope struct {
	Codec string
	// Encryption is "none" unless WithEncryptionKey is supplied.
	Encryption    string
	encryptionKey []byte
	// now returns the Created timestamp. Defaults to reproducibleNow, which
	// honors SOURCE_DATE_EPOCH so packs can be byte-identical across runs.
	now func() time.Time
}

// EnvelopeOption customizes an envelope renderer.
type EnvelopeOption func(*Envelope) error

// NewEnvelope returns the envelope renderer for the given codec.
func NewEnvelope(codec string, opts ...EnvelopeOption) (*Envelope, error) {
	if codec != CodecGzip && codec != CodecZstd {
		return nil, fmt.Errorf("unsupported codec %q (valid: %s, %s)", codec, CodecGzip, CodecZstd)
	}
	e := &Envelope{Codec: codec, Encryption: EncryptionNone, now: reproducibleNow}
	for _, opt := range opts {
		if err := opt(e); err != nil {
			return nil, err
		}
	}
	return e, nil
}

// WithEncryptionKey encrypts the compressed payload with AES-256-GCM. The key
// must be exactly 32 bytes; DecodeAES256KeyHex is a CLI-friendly helper.
func WithEncryptionKey(key []byte) EnvelopeOption {
	return func(e *Envelope) error {
		if len(key) != aes256KeyBytes {
			return fmt.Errorf("AES-256-GCM key must be %d bytes, got %d", aes256KeyBytes, len(key))
		}
		e.Encryption = EncryptionAES256GCM
		e.encryptionKey = append([]byte(nil), key...)
		return nil
	}
}

// DecodeAES256KeyHex decodes the 64-character hex key expected by CLI env
// vars. It keeps passphrases out of process args and shell histories.
func DecodeAES256KeyHex(value string) ([]byte, error) {
	key, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode AES key hex: %w", err)
	}
	if len(key) != aes256KeyBytes {
		return nil, fmt.Errorf("AES-256-GCM key must decode to %d bytes, got %d", aes256KeyBytes, len(key))
	}
	return key, nil
}

// Wire types -- the NDJSON payload schema. Field order is the wire order
// (encoding/json preserves struct order), so changing it is a format change.

type wireManifest struct {
	Type       string          `json:"type"` // "manifest"
	Root       string          `json:"root"`
	Files      int             `json:"files"`
	Skipped    int             `json:"skipped"`
	Skips      []wireSkip      `json:"skips"`
	Redactions []wireRedaction `json:"redactions,omitempty"`
	Order      string          `json:"order"`     // "path-asc"
	HashAlgo   string          `json:"hash_algo"` // "sha256"
}

type wireSkip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
	Size   int64  `json:"size,omitempty"`
}

type wireRedaction struct {
	Path  string `json:"path"`
	Rule  string `json:"rule"`
	Count int    `json:"count"`
}

type wireFile struct {
	Type   string `json:"type"` // "file"
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Lang   string `json:"lang,omitempty"`
	// Content is a pointer so an empty file ("") is distinguishable from an
	// elided duplicate (nil + dup_of).
	Content *string `json:"content,omitempty"`
	DupOf   string  `json:"dup_of,omitempty"`
}

func (e *Envelope) Render(w io.Writer, p *model.Pack) error {
	payload, err := os.CreateTemp("", "codepack-lz-payload-*")
	if err != nil {
		return err
	}
	defer os.Remove(payload.Name())
	defer payload.Close()

	cw := &countingWriter{w: payload}
	zw, err := e.compressor(cw)
	if err != nil {
		return err
	}
	if err := e.encodePayloadTo(zw, p); err != nil {
		_ = zw.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if _, err := payload.Seek(0, io.SeekStart); err != nil {
		return err
	}

	var payloadReader io.Reader = payload
	bytesPacked := cw.n
	nonce := ""
	if e.Encryption == EncryptionAES256GCM {
		compressed, err := io.ReadAll(payload)
		if err != nil {
			return err
		}
		ciphertext, nonceBytes, err := encryptAESGCM(e.encryptionKey, compressed)
		if err != nil {
			return err
		}
		payloadReader = bytes.NewReader(ciphertext)
		bytesPacked = int64(len(ciphertext))
		nonce = base64.StdEncoding.EncodeToString(nonceBytes)
	}

	// Plaintext header -- readable (and greppable) without decoding.
	fmt.Fprintf(w, "%s\n", MagicLine)
	fmt.Fprintf(w, "Format-Version: %d\n", version.FormatVersion)
	fmt.Fprintf(w, "Tool-Version: %s\n", version.Version)
	fmt.Fprintf(w, "Encoding: base64\n")
	fmt.Fprintf(w, "Codec: %s\n", e.Codec)
	fmt.Fprintf(w, "Encryption: %s\n", e.Encryption)
	if nonce != "" {
		fmt.Fprintf(w, "Nonce: %s\n", nonce)
	}
	fmt.Fprintf(w, "Created: %s\n", e.now().UTC().Format(time.RFC3339))
	fmt.Fprintf(w, "Root: %s\n", p.Root)
	fmt.Fprintf(w, "Files: %d\n", len(p.Files))
	fmt.Fprintf(w, "Skipped: %d\n", len(p.Skips))
	fmt.Fprintf(w, "Bytes-Raw: %d\n", p.TotalBytes)
	fmt.Fprintf(w, "Bytes-Packed: %d\n", bytesPacked)
	fmt.Fprintf(w, "Hash-Algo: sha256\n")
	fmt.Fprintf(w, "Secret-Scan: %s\n", p.SecretScan)
	fmt.Fprintf(w, "Warning: %s\n", e.warning())
	fmt.Fprintf(w, "\n%s\n", BeginMarker)

	if err := writeBase64WrappedReader(w, payloadReader); err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", EndMarker)
	return err
}

// encodePayload builds the NDJSON payload: manifest first, then files in
// their already-sorted order.
func (e *Envelope) encodePayload(p *model.Pack) ([]byte, error) {
	var buf bytes.Buffer
	if err := e.encodePayloadTo(&buf, p); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// encodePayloadTo streams the NDJSON payload: manifest first, then files in
// their already-sorted order.
func (e *Envelope) encodePayloadTo(w io.Writer, p *model.Pack) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false) // source code full of < > & must stay readable

	manifest := wireManifest{
		Type:     "manifest",
		Root:     p.Root,
		Files:    len(p.Files),
		Skipped:  len(p.Skips),
		Skips:    make([]wireSkip, 0, len(p.Skips)),
		Order:    "path-asc",
		HashAlgo: "sha256",
	}
	for _, s := range p.Skips {
		manifest.Skips = append(manifest.Skips, wireSkip{Path: s.Path, Reason: s.Reason, Size: s.Size})
	}
	for _, r := range p.Redactions {
		manifest.Redactions = append(manifest.Redactions, wireRedaction{Path: r.Path, Rule: r.Rule, Count: r.Count})
	}
	if err := enc.Encode(manifest); err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}

	for i := range p.Files {
		f := &p.Files[i]
		wf := wireFile{Type: "file", Path: f.Path, Size: f.Size, SHA256: f.SHA256, Lang: f.Lang}
		if f.DupOf != "" {
			wf.DupOf = f.DupOf
		} else {
			content := string(f.Content)
			wf.Content = &content
		}
		if err := enc.Encode(wf); err != nil {
			return fmt.Errorf("encode %s: %w", f.Path, err)
		}
	}
	return nil
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func (e *Envelope) compressor(w io.Writer) (io.WriteCloser, error) {
	switch e.Codec {
	case CodecGzip:
		// gzip.Writer with a zero ModTime writes mtime=0 and Go always writes
		// OS=255, so compression output is deterministic for identical input.
		return gzip.NewWriterLevel(w, gzip.BestCompression)
	case CodecZstd:
		return zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	default:
		return nil, fmt.Errorf("unsupported codec %q", e.Codec)
	}
}

func (e *Envelope) warning() string {
	if e.Encryption == EncryptionAES256GCM {
		return EncryptedWarning
	}
	return SecurityWarning
}

func writeBase64WrappedReader(w io.Writer, r io.Reader) error {
	buf := make([]byte, base64LineWidth/4*3) // 57 raw bytes encode to 76 columns.
	for {
		n, err := io.ReadFull(r, buf)
		if err == io.EOF {
			return nil
		}
		if err == io.ErrUnexpectedEOF {
			if n == 0 {
				return nil
			}
			if _, werr := io.WriteString(w, base64.StdEncoding.EncodeToString(buf[:n])); werr != nil {
				return werr
			}
			_, werr := io.WriteString(w, "\n")
			return werr
		}
		if err != nil {
			return err
		}
		if _, err := io.WriteString(w, base64.StdEncoding.EncodeToString(buf[:n])); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "\n"); err != nil {
			return err
		}
	}
}

func encryptAESGCM(key, plaintext []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, aesGCMNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return gcm.Seal(nil, nonce, plaintext, nil), nonce, nil
}

// reproducibleNow returns the SOURCE_DATE_EPOCH time when set (the
// reproducible-builds.org convention), else the current time. Everything
// below the Created header is already deterministic; honoring this variable
// makes the entire envelope byte-reproducible.
func reproducibleNow() time.Time {
	if v := os.Getenv("SOURCE_DATE_EPOCH"); v != "" {
		if sec, err := strconv.ParseInt(v, 10, 64); err == nil {
			return time.Unix(sec, 0)
		}
	}
	return time.Now()
}
