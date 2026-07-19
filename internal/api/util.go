package api

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// writeJSON encodes v as a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr sends {"error": msg} with the given status.
func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decodeJSON reads a JSON body into dst, capping the body size.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// bearer extracts a token from the Authorization header or ?token= query. The
// query fallback lets <img>/<video> tags load protected files (they can't set
// headers).
func bearer(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

// kindFor classifies a MIME type into the coarse buckets the feed UI renders.
func kindFor(contentType string) string {
	switch {
	case strings.HasPrefix(contentType, "image/"):
		return "image"
	case strings.HasPrefix(contentType, "video/"):
		return "video"
	default:
		return "file"
	}
}

// contentTypeFor resolves an upload's MIME type, falling back to the extension.
func contentTypeFor(hdrContentType, filename string) string {
	if hdrContentType != "" && hdrContentType != "application/octet-stream" {
		return hdrContentType
	}
	if ext := filepath.Ext(filename); ext != "" {
		if ct := mime.TypeByExtension(ext); ct != "" {
			return ct
		}
	}
	if hdrContentType != "" {
		return hdrContentType
	}
	return "application/octet-stream"
}

// saveFile streams an upload to <uploadDir>/<id> (no extension — the content
// type is authoritative and stored in the DB).
func (h *Handler) saveFile(id string, src io.Reader) (int64, error) {
	if err := os.MkdirAll(h.uploadDir, 0o755); err != nil {
		return 0, err
	}
	dst, err := os.Create(filepath.Join(h.uploadDir, id))
	if err != nil {
		return 0, err
	}
	defer dst.Close()
	return io.Copy(dst, src)
}
