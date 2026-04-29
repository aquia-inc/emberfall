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
)

// maxMultipartFileSize caps in-memory buffering of any single uploaded file to
// guard against runaway YAML referencing /dev/urandom, sparse files, or other
// large inputs that would balloon the request buffer.
const maxMultipartFileSize = 50 * 1024 * 1024 // 50 MiB

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

// validatePartName rejects names that would let a YAML author inject extra
// MIME headers via CR, LF, or NUL bytes in the part's Content-Disposition.
func validatePartName(kind, name string) error {
	if strings.ContainsAny(name, "\r\n\x00") {
		return fmt.Errorf("multipart %s name contains forbidden control characters: %q", kind, name)
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
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, filepath.Base(path)))
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
