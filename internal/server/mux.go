package server

import (
	"bytes"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

func newMux(api http.Handler, web fs.FS, anonymousToken string) (http.Handler, error) {
	index, err := fs.ReadFile(web, "index.html")
	if err != nil {
		return nil, err
	}
	api = anonymousRead(api, anonymousToken)
	files := http.FileServerFS(web)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1" || strings.HasPrefix(r.URL.Path, "/v1/") {
			api.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" && name != "." {
			if info, statErr := fs.Stat(web, name); statErr == nil && !info.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, "index.html", zeroTime, bytes.NewReader(index))
	}), nil
}

func anonymousRead(next http.Handler, token string) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
			r.URL.Path != "/v1/health" &&
			r.Header.Get("Authorization") == "" && r.Header.Get("X-Commons-Host-Credential") == "" {
			clone := r.Clone(r.Context())
			clone.Header = r.Header.Clone()
			clone.Header.Set("Authorization", "Bearer "+token)
			next.ServeHTTP(w, clone)
			return
		}
		next.ServeHTTP(w, r)
	})
}
