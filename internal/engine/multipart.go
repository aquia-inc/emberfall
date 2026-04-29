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
)

// writeMultipart writes a multipart/form-data payload into buf and returns the
// Content-Type (including the generated boundary). File parts use a Content-Type
// derived from the file extension, falling back to application/octet-stream.
// File paths are resolved relative to the process working directory.
func writeMultipart(buf *bytes.Buffer, m *multipartBody) (string, error) {
	w := multipart.NewWriter(buf)

	for k, v := range m.Fields {
		if err := w.WriteField(k, v); err != nil {
			return "", err
		}
	}

	for field, path := range m.Files {
		if err := writeFilePart(w, field, path); err != nil {
			return "", err
		}
	}

	if err := w.Close(); err != nil {
		return "", err
	}
	return w.FormDataContentType(), nil
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
	if _, err := io.Copy(part, f); err != nil {
		return err
	}
	return nil
}
