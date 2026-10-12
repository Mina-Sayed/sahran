package main

import (
	"embed"
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

//go:embed web/index.html
var webFS embed.FS

func (e *Engine) server(addr string) *http.Server {
	index, _ := webFS.ReadFile("web/index.html")
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, e.Snapshot())
	})
	mux.HandleFunc("POST /api/mode", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
			return
		}
		if err := e.SetMode(body.Mode); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"mode": body.Mode})
	})
	mux.HandleFunc("POST /api/dismiss", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
			return
		}
		n, err := e.DismissName(body.Name)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]int{"ignored": n})
	})
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, e.getCfg())
	})
	mux.HandleFunc("POST /api/config", func(w http.ResponseWriter, r *http.Request) {
		c := e.getCfg() // start from current so missing fields keep their values
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(&c); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
			return
		}
		if c.HotspotConn != "" && c.HotspotConn == c.PreferredConn {
			writeJSON(w, 400, map[string]string{"error": "Hotspot and home Wi-Fi must be two different connections."})
			return
		}
		if err := e.SetConfig(c); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, e.getCfg())
	})
	mux.HandleFunc("GET /api/connections", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, listConns())
	})

	return &http.Server{Addr: addr, Handler: guard(mux)}
}

// guard blocks DNS-rebinding (Host check) and cross-site POSTs (custom header
// forces a CORS preflight, which we never answer).
// Rejections are JSON so the CLI and dashboard can show the reason.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'")
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		host = strings.Trim(host, "[]")
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden host"})
			return
		}
		if r.Method == http.MethodPost && r.Header.Get("X-Sahran") != "1" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "missing X-Sahran header"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
