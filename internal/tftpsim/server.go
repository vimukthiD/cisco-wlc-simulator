package tftpsim

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pin/tftp/v3"
	"golang.org/x/sys/unix"

	"github.com/vimukthi/cisco-wlc-sim/internal/accesslog"
	"github.com/vimukthi/cisco-wlc-sim/internal/device"
)

const idleTimeout = 20 * time.Second

// Manager handles on-demand TFTP server startup per device.
// Servers start lazily on first use and shut down after 20s of inactivity.
type Manager struct {
	logs *accesslog.Store
	mu   sync.Mutex
	inst map[string]*instance // keyed by device IP
}

type instance struct {
	server   *tftp.Server
	conn     net.PacketConn
	timer    *time.Timer
	mu       sync.Mutex
}

func (inst *instance) touch() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.timer.Reset(idleTimeout)
}

// NewManager creates a TFTP manager.
func NewManager(logs *accesslog.Store) *Manager {
	return &Manager{
		logs: logs,
		inst: make(map[string]*instance),
	}
}

// EnsureRunning starts a TFTP server for the device if not already running.
// It blocks until the server is listening.
func (m *Manager) EnsureRunning(dev *device.Device) {
	port := dev.TFTPPort
	if port == 0 {
		port = 69
	}

	m.mu.Lock()
	if inst, ok := m.inst[dev.IP]; ok {
		inst.touch()
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	ready := make(chan struct{})
	go m.run(dev, port, ready)
	<-ready
}

func (m *Manager) run(dev *device.Device, port int, ready chan struct{}) {
	addr := fmt.Sprintf("%s:%d", dev.IP, port)

	var inst instance
	inst.timer = time.NewTimer(idleTimeout)

	activity := func() { inst.touch() }

	readHandler := func(filename string, rf io.ReaderFrom) error {
		activity()
		var config string
		if strings.Contains(filename, "startup") {
			config = dev.StartupConfig()
		} else {
			config = dev.RunningConfig()
		}
		m.logs.Add(accesslog.Entry{
			DeviceHost: dev.Hostname,
			DeviceIP:   dev.IP,
			Type:       "tftp",
			Method:     "READ",
			Path:       filename,
		})
		rf.(tftp.OutgoingTransfer).SetSize(int64(len(config)))
		_, err := rf.ReadFrom(strings.NewReader(config))
		return err
	}

	writeHandler := func(filename string, wt io.WriterTo) error {
		activity()
		m.logs.Add(accesslog.Entry{
			DeviceHost: dev.Hostname,
			DeviceIP:   dev.IP,
			Type:       "tftp",
			Method:     "WRITE",
			Path:       filename,
		})
		_, err := wt.WriteTo(io.Discard)
		return err
	}

	server := tftp.NewServer(readHandler, writeHandler)
	server.SetTimeout(5 * time.Second)
	server.EnableSinglePort()

	conn, err := listenUDPReuse(addr)
	if err != nil {
		close(ready)
		log.Printf("[%s] TFTP server error: %v", dev.Hostname, err)
		return
	}
	inst.server = server
	inst.conn = conn

	m.mu.Lock()
	m.inst[dev.IP] = &inst
	m.mu.Unlock()

	log.Printf("[%s] TFTP started on-demand at %s", dev.Hostname, addr)
	close(ready)

	// Serve in background, shut down on idle timeout
	done := make(chan struct{})
	go func() {
		server.Serve(conn)
		close(done)
	}()

	select {
	case <-inst.timer.C:
		// Idle timeout — shut down
	case <-done:
		// Server stopped on its own
	}

	server.Shutdown()
	conn.Close()

	m.mu.Lock()
	delete(m.inst, dev.IP)
	m.mu.Unlock()

	log.Printf("[%s] TFTP stopped (idle timeout) at %s", dev.Hostname, addr)
}

func listenUDPReuse(address string) (net.PacketConn, error) {
	lc := net.ListenConfig{
		Control: func(network, addr string, c syscall.RawConn) error {
			var opErr error
			err := c.Control(func(fd uintptr) {
				opErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
				if opErr != nil {
					return
				}
				opErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
			})
			if err != nil {
				return err
			}
			return opErr
		},
	}
	return lc.ListenPacket(context.Background(), "udp", address)
}
