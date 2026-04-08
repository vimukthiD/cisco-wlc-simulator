package dashboard

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/vimukthi/cisco-wlc-sim/internal/accesslog"
	"github.com/vimukthi/cisco-wlc-sim/internal/config"
)

//go:embed static/*
var staticFS embed.FS

// Serve starts the dashboard HTTP server.
func Serve(port int, cfg *config.Config, logs *accesslog.Store) error {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/devices", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, cfg.Devices)
	})

	mux.HandleFunc("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, logs.Recent(500))
	})

	// SSE endpoint for live log streaming
	mux.HandleFunc("/api/logs/stream", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		flusher.Flush()

		ch := logs.Subscribe()
		defer logs.Unsubscribe(ch)

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case entry, ok := <-ch:
				if !ok {
					return
				}
				data, _ := json.Marshal(entry)
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	})

	// Serve the embedded static files
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			data, err := staticFS.ReadFile("static/index.html")
			if err != nil {
				http.Error(w, "not found", 404)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(data)
			return
		}
		http.FileServer(http.FS(staticFS)).ServeHTTP(w, r)
	})

	addr := fmt.Sprintf(":%d", port)
	log.Printf("[Dashboard] listening on http://localhost%s", addr)
	return http.ListenAndServe(addr, mux)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}
