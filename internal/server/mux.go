package server

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"

	"codex-commons/internal/httpapi"
)

func contentSecurityPolicy(index []byte) string {
	const opening = "<script>"
	const closing = "</script>"
	start := bytes.Index(index, []byte(opening))
	if start < 0 {
		return "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
	}
	start += len(opening)
	end := bytes.Index(index[start:], []byte(closing))
	if end < 0 {
		return "default-src 'none'; frame-ancestors 'none'"
	}
	digest := sha256.Sum256(index[start : start+end])
	hash := "'sha256-" + base64.StdEncoding.EncodeToString(digest[:]) + "'"
	return "default-src 'self'; script-src 'self' " + hash + "; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
}

func setBrowserSecurityHeaders(header http.Header, csp string) {
	header.Set("Content-Security-Policy", csp)
	header.Set("Cross-Origin-Opener-Policy", "same-origin")
	header.Set("Cross-Origin-Resource-Policy", "same-origin")
	header.Set("Permissions-Policy", "camera=(), geolocation=(), microphone=(), payment=(), usb=()")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
}

func acceptsGzip(value string) bool {
	gzipQuality, wildcardQuality := 0.0, 0.0
	gzipSpecified := false
	for _, raw := range strings.Split(value, ",") {
		parts := strings.Split(raw, ";")
		name := strings.TrimSpace(parts[0])
		quality := 1.0
		for _, parameter := range parts[1:] {
			key, rawValue, found := strings.Cut(strings.TrimSpace(parameter), "=")
			if found && strings.EqualFold(strings.TrimSpace(key), "q") {
				parsed, err := strconv.ParseFloat(strings.TrimSpace(rawValue), 64)
				if err != nil {
					quality = 0
				} else {
					quality = parsed
				}
			}
		}
		if strings.EqualFold(name, "gzip") {
			gzipSpecified, gzipQuality = true, quality
		} else if name == "*" {
			wildcardQuality = quality
		}
	}
	if gzipSpecified {
		return gzipQuality > 0
	}
	return wildcardQuality > 0
}

func compressibleAsset(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".css", ".js", ".json", ".svg", ".txt":
		return true
	default:
		return false
	}
}

func serveCompressedAsset(w http.ResponseWriter, r *http.Request, web fs.FS, cache *sync.Map, name string, info fs.FileInfo) bool {
	if !strings.HasPrefix(name, "assets/") || !compressibleAsset(name) || info.Size() > 8<<20 {
		return false
	}
	w.Header().Add("Vary", "Accept-Encoding")
	if !acceptsGzip(r.Header.Get("Accept-Encoding")) || r.Header.Get("Range") != "" {
		return false
	}
	cached, ok := cache.Load(name)
	if !ok {
		plain, err := fs.ReadFile(web, name)
		if err != nil {
			return false
		}
		var encoded bytes.Buffer
		writer, err := gzip.NewWriterLevel(&encoded, gzip.BestCompression)
		if err != nil {
			return false
		}
		if _, err = writer.Write(plain); err != nil || writer.Close() != nil {
			return false
		}
		cached, _ = cache.LoadOrStore(name, encoded.Bytes())
	}
	compressed, ok := cached.([]byte)
	if !ok {
		return false
	}
	w.Header().Set("Content-Encoding", "gzip")
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, path.Base(name), info.ModTime(), bytes.NewReader(compressed))
	return true
}

func newMux(api http.Handler, web fs.FS, anonymousToken, expectedHost string) (http.Handler, error) {
	index, err := fs.ReadFile(web, "index.html")
	if err != nil {
		return nil, err
	}
	api = anonymousRead(api, anonymousToken)
	files := http.FileServerFS(web)
	var compressedAssets sync.Map
	csp := contentSecurityPolicy(index)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setBrowserSecurityHeaders(w.Header(), csp)
		internalReadiness := r.URL.Path == "/v1/internal/readiness"
		if expectedHost != "" && !strings.EqualFold(r.Host, expectedHost) && !internalReadiness {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "misdirected request", http.StatusMisdirectedRequest)
			return
		}
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
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				if serveCompressedAsset(w, r, web, &compressedAssets, name, info) {
					return
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		if name != "" && name != "." && (strings.HasPrefix(name, "assets/") || path.Ext(name) != "") {
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w, r)
			return
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
		_, humanCookieErr := r.Cookie(httpapi.HumanSessionCookieName)
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
			r.URL.Path != "/v1/health" &&
			r.Header.Get("Authorization") == "" && r.Header.Get("X-Commons-Host-Credential") == "" &&
			humanCookieErr != nil {
			clone := r.Clone(r.Context())
			clone.Header = r.Header.Clone()
			clone.Header.Set("Authorization", "Bearer "+token)
			next.ServeHTTP(w, clone)
			return
		}
		next.ServeHTTP(w, r)
	})
}
