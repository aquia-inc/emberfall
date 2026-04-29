package engine

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// maxMultipartFileSize caps the in-memory buffering of each individual file part
// so a YAML pointing at /dev/urandom, a sparse file, or any unexpectedly large
// input cannot balloon the request buffer. The limit is per file, not aggregate
// across all files in a single test; configurations with many large attachments
// should still be sized with the operator's available memory in mind.
const maxMultipartFileSize = 50 * 1024 * 1024 // 50 MiB

// quoteEscaper mirrors the stdlib's mime/multipart.escapeQuotes (which is
// unexported) so that field and filename values are quoted the same way
// CreateFormFile would do it. Non-ASCII bytes are passed through as raw UTF-8,
// which lenient servers parse correctly. Strict RFC 5987 (filename*=UTF-8''...)
// encoding is not implemented; non-ASCII filenames may not interoperate with
// servers that reject anything outside the legacy quoted-string form.
var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

// writeMultipart writes a multipart/form-data payload into buf and returns the
// Content-Type (including the generated boundary). File parts use a Content-Type
// derived from the file extension, falling back to application/octet-stream.
// File paths are resolved relative to the process working directory.
func writeMultipart(buf *bytes.Buffer, m *multipartBody) (string, error) {
	w := multipart.NewWriter(buf)

	for k, v := range m.Fields {
		if err := validatePartName("field", k); err != nil {
			return "", err
		}
		if err := w.WriteField(k, v); err != nil {
			return "", err
		}
	}

	for field, path := range m.Files {
		if err := validatePartName("file form name", field); err != nil {
			return "", err
		}
		if err := writeFilePart(w, field, path); err != nil {
			return "", err
		}
	}

	if err := w.Close(); err != nil {
		return "", err
	}
	return w.FormDataContentType(), nil
}

// validatePartName rejects names containing any control character so a YAML
// author cannot inject extra MIME headers (via CR/LF/NUL) or produce
// syntactically invalid Content-Disposition parameters (via other control
// bytes that bypass the quoted-string escape but still violate RFC 7230).
func validatePartName(kind, name string) error {
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("multipart %s name contains forbidden control characters: %q", kind, name)
		}
	}
	return nil
}

func writeFilePart(w *multipart.Writer, field, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("multipart file %q: %w", path, err)
	}
	defer f.Close()

	ctype := mime.TypeByExtension(filepath.Ext(path))
	if ctype == "" {
		ctype = "application/octet-stream"
	}

	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`,
		quoteEscaper.Replace(field),
		quoteEscaper.Replace(filepath.Base(path))))
	h.Set("Content-Type", ctype)

	part, err := w.CreatePart(h)
	if err != nil {
		return err
	}

	limited := &io.LimitedReader{R: f, N: maxMultipartFileSize + 1}
	n, err := io.Copy(part, limited)
	if err != nil {
		return err
	}
	if n > maxMultipartFileSize {
		return fmt.Errorf("multipart file %q exceeds %d byte limit", path, maxMultipartFileSize)
	}
	return nil
}
