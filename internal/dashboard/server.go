package dashboard

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/vimukthiD/cisco-wlc-simulator/internal/accesslog"
	"github.com/vimukthiD/cisco-wlc-simulator/internal/device"
	"github.com/vimukthiD/cisco-wlc-simulator/internal/simulator"
)

var startTime = time.Now()

// cpuSampler tracks process CPU usage by sampling getrusage periodically.
type cpuSampler struct {
	mu      sync.Mutex
	cpuPct  float64
	lastCPU time.Duration
	lastAt  time.Time
}

func newCPUSampler() *cpuSampler {
	s := &cpuSampler{
		lastCPU: getProcessCPUTime(),
		lastAt:  time.Now(),
	}
	go s.loop()
	return s
}

func (s *cpuSampler) loop() {
	ticker := time.NewTicker(2 * time.Second)
	for range ticker.C {
		now := time.Now()
		cpuNow := getProcessCPUTime()

		s.mu.Lock()
		wall := now.Sub(s.lastAt).Seconds()
		cpu := (cpuNow - s.lastCPU).Seconds()
		if wall > 0 {
			s.cpuPct = (cpu / wall) * 100
		}
		s.lastCPU = cpuNow
		s.lastAt = now
		s.mu.Unlock()
	}
}

func (s *cpuSampler) Usage() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cpuPct
}

func getProcessCPUTime() time.Duration {
	var rusage syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &rusage)
	user := time.Duration(rusage.Utime.Sec)*time.Second + time.Duration(rusage.Utime.Usec)*time.Microsecond
	sys := time.Duration(rusage.Stime.Sec)*time.Second + time.Duration(rusage.Stime.Usec)*time.Microsecond
	return user + sys
}

//go:embed static/*
var staticFS embed.FS

// Serve starts the dashboard HTTP server.
func Serve(port int, sim *simulator.Simulator, logs *accesslog.Store) error {
	mux := http.NewServeMux()
	cpu := newCPUSampler()

	// API endpoints
	mux.HandleFunc("/api/system", func(w http.ResponseWriter, r *http.Request) {
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		writeJSON(w, map[string]any{
			"uptime_secs":    int(time.Since(startTime).Seconds()),
			"goroutines":     runtime.NumGoroutine(),
			"cpu_count":      runtime.NumCPU(),
			"cpu_pct":        cpu.Usage(),
			"mem_alloc":      mem.Alloc,
			"mem_sys":        mem.Sys,
			"mem_heap_alloc": mem.HeapAlloc,
			"mem_heap_sys":   mem.HeapSys,
			"gc_cycles":      mem.NumGC,
			"gc_pause_ns":    mem.PauseTotalNs,
		})
	})

	mux.HandleFunc("/api/auth", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, sim.Auth())
	})

	mux.HandleFunc("/api/devices", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET", "":
			writeJSON(w, sim.Devices())
		case "POST":
			var dev device.Device
			if err := json.NewDecoder(r.Body).Decode(&dev); err != nil {
				http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
				return
			}
			if err := sim.AddDevice(dev); err != nil {
				http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusConflict)
				return
			}
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, dev)
		case "DELETE":
			ip := r.URL.Query().Get("ip")
			if ip == "" {
				http.Error(w, `{"error":"ip query parameter required"}`, http.StatusBadRequest)
				return
			}
			if err := sim.RemoveDevice(ip); err != nil {
				http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusNotFound)
				return
			}
			writeJSON(w, map[string]string{"status": "removed", "ip": ip})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/devices/ap", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST":
			var req struct {
				DeviceIP string    `json:"device_ip"`
				AP       device.AP `json:"ap"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
				return
			}
			if req.DeviceIP == "" || req.AP.Name == "" {
				http.Error(w, `{"error":"device_ip and ap.name are required"}`, http.StatusBadRequest)
				return
			}
			if err := sim.AddAP(req.DeviceIP, req.AP); err != nil {
				http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, req.AP)
		case "DELETE":
			ip := r.URL.Query().Get("device_ip")
			name := r.URL.Query().Get("name")
			if ip == "" || name == "" {
				http.Error(w, `{"error":"device_ip and name query parameters required"}`, http.StatusBadRequest)
				return
			}
			if err := sim.RemoveAP(ip, name); err != nil {
				http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusNotFound)
				return
			}
			writeJSON(w, map[string]string{"status": "removed"})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/devices/client", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST":
			var req struct {
				DeviceIP string        `json:"device_ip"`
				APName   string        `json:"ap_name"`
				Client   device.Client `json:"client"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
				return
			}
			if req.DeviceIP == "" || req.APName == "" || req.Client.MAC == "" {
				http.Error(w, `{"error":"device_ip, ap_name, and client.mac are required"}`, http.StatusBadRequest)
				return
			}
			if err := sim.AddClient(req.DeviceIP, req.APName, req.Client); err != nil {
				http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, req.Client)
		case "DELETE":
			ip := r.URL.Query().Get("device_ip")
			mac := r.URL.Query().Get("mac")
			if ip == "" || mac == "" {
				http.Error(w, `{"error":"device_ip and mac query parameters required"}`, http.StatusBadRequest)
				return
			}
			if err := sim.RemoveClient(ip, mac); err != nil {
				http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusNotFound)
				return
			}
			writeJSON(w, map[string]string{"status": "removed"})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/devices/ap/ssids", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			DeviceIP string   `json:"device_ip"`
			APName   string   `json:"ap_name"`
			SSIDs    []string `json:"ssids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
			return
		}
		if err := sim.UpdateAPSSIDs(req.DeviceIP, req.APName, req.SSIDs); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"status": "updated", "ssids": req.SSIDs})
	})

	mux.HandleFunc("/api/devices/client/move", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			DeviceIP  string `json:"device_ip"`
			ClientMAC string `json:"client_mac"`
			NewAP     string `json:"new_ap"`
			NewSSID   string `json:"new_ssid"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
			return
		}
		if err := sim.UpdateClient(req.DeviceIP, req.ClientMAC, req.NewAP, req.NewSSID); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]string{"status": "moved"})
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
