// Package hub: loopback enforcement, Host/Origin validation, and CSRF token guard.
package hub

import (
	"bytes"
	"crypto/subtle"
	"html"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"
)

func (s *Server) serveIndexHTML(w http.ResponseWriter, r *http.Request) bool {
	if s.frontendFS == nil {
		return false
	}
	raw, err := fs.ReadFile(s.frontendFS, "index.html")
	if err != nil {
		return false
	}
	if s.authToken != "" {
		metaTag := []byte(`<meta name="agymate-token" content="` + html.EscapeString(s.authToken) + `">` + "\n")
		if idx := bytes.Index(bytes.ToLower(raw), []byte("</head>")); idx >= 0 {
			out := make([]byte, 0, len(raw)+len(metaTag))
			out = append(out, raw[:idx]...)
			out = append(out, metaTag...)
			out = append(out, raw[idx:]...)
			raw = out
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(raw)
	}
	return true
}

func (s *Server) hasValidAuthToken(r *http.Request) bool {
	if s.authToken == "" {
		return false
	}
	provided := strings.TrimSpace(r.Header.Get("X-AgyMate-Token"))
	if provided == "" {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		if len(auth) > 7 && strings.EqualFold(auth[:7], "bearer ") {
			provided = strings.TrimSpace(auth[7:])
		}
	}
	if len(provided) != len(s.authToken) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(s.authToken)) == 1
}

func isPublicNullOriginReadPath(path string) bool {
	switch path {
	case "/api/health", "/api/overview", "/api/events":
		return true
	default:
		return false
	}
}

func (s *Server) securityGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackRemote(r.RemoteAddr) {
			writeJSONError(w, http.StatusForbidden, "forbidden: loopback access only")
			return
		}
		if !isAllowedHost(r.Host, s.Config().ListenAddr) {
			writeJSONError(w, http.StatusForbidden, "forbidden: invalid host")
			return
		}
		origin := r.Header.Get("Origin")
		isOAuthCallback := r.Method == http.MethodGet && r.URL.Path == "/api/colab/oauth/callback"
		if origin != "" && !isOAuthCallback {
			if !isLoopbackOrigin(origin) {
				writeJSONError(w, http.StatusForbidden, "forbidden origin")
				return
			}
			allowCORS := strings.HasPrefix(r.URL.Path, "/api/")
			if allowCORS && (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
				strings.EqualFold(strings.TrimSpace(origin), "null") &&
				!isPublicNullOriginReadPath(r.URL.Path) && !s.hasValidAuthToken(r) {
				allowCORS = false
			}
			if allowCORS {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Sidecar-Token, X-AgyMate-Token, Authorization")
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !s.hasValidAuthToken(r) {
				writeJSONError(w, http.StatusUnauthorized, "unauthorized: missing or invalid X-AgyMate-Token")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackRemote(remoteAddr string) bool {
	if remoteAddr == "" {
		return true
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func isAllowedHost(host, listenAddr string) bool {
	if host == listenAddr {
		return true
	}
	parts := strings.Split(host, ":")
	if len(parts) == 2 {
		ip := parts[0]
		port := parts[1]
		isPort := true
		for _, c := range port {
			if c < '0' || c > '9' {
				isPort = false
				break
			}
		}
		if isPort && (ip == "127.0.0.1" || ip == "localhost") {
			return true
		}
	}
	return host == "127.0.0.1" || host == "localhost"
}

func isLoopbackOrigin(origin string) bool {
	low := strings.ToLower(strings.TrimSpace(origin))
	if low == "null" {
		return true
	}
	for _, prefix := range []string{
		"wails://",
		"magpie://",
		"app://",
		"tauri://",
		"vscode-webview://",
		"antigravity://",
		"file://",
	} {
		if strings.HasPrefix(low, prefix) {
			return true
		}
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}
