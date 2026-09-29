package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"file-cleaner/internal/filesystem"
)

// writeJSON writes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// writeError writes a uniform error payload.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// fail maps internal errors to a stable HTTP status.
func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, filesystem.ErrOutsideRoot):
		writeError(w, http.StatusForbidden, "路径超出允许访问的根目录")
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

// decodeJSON reads a bounded JSON request body. The bound is generous because
// a delete/re-preview request carries one path per selected entry; the web UI
// additionally chunks very large selections.
func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errors.New("请求体解析失败: " + err.Error())
	}
	return nil
}

// maxBodyBytes is the largest accepted JSON request body (64 MiB).
const maxBodyBytes = 64 << 20

func qs(r *http.Request, key, def string) string {
	if v := strings.TrimSpace(r.URL.Query().Get(key)); v != "" {
		return v
	}
	return def
}

func qi(r *http.Request, key string, def int) int {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// qi64 returns def when the parameter is absent; -1 means "explicitly unset"
// is never returned, callers use has() when they need to distinguish.
func qi64(r *http.Request, key string, def int64) int64 {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func qbool(r *http.Request, key string, def bool) bool {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

// splitCSV splits "a,b, c" into ["a","b","c"].
func splitCSV(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
