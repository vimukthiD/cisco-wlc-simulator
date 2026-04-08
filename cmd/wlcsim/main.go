package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vimukthi/cisco-wlc-sim/internal/accesslog"
	"github.com/vimukthi/cisco-wlc-sim/internal/config"
	"github.com/vimukthi/cisco-wlc-sim/internal/dashboard"
	"github.com/vimukthi/cisco-wlc-sim/internal/network"
	"github.com/vimukthi/cisco-wlc-sim/internal/simulator"
)

func main() {
	configPath := flag.String("config", "configs/devices.yaml", "path to devices config file")
	dashPort := flag.Int("dashboard-port", 8080, "web dashboard port")
	lanMode := flag.Bool("lan", false, "bind to physical interface for LAN accessibility")
	lanIface := flag.String("interface", "", "network interface for LAN mode (auto-detect if empty)")
	setupOnly := flag.Bool("setup-ips", false, "only add virtual IP aliases, then exit")
	teardownOnly := flag.Bool("teardown-ips", false, "only remove virtual IP aliases, then exit")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Standalone IP management commands
	if *teardownOnly {
		log.Println("Removing virtual IP aliases...")
		network.TeardownIPs(cfg.Devices)
		log.Println("Done.")
		return
	}
	if *setupOnly {
		log.Println("Setting up virtual IP aliases...")
		if err := network.SetupIPs(cfg.Devices); err != nil {
			log.Fatalf("Failed to setup IPs: %v", err)
		}
		log.Println("Done.")
		return
	}

	// Set up IPs — LAN mode or local-only
	var lanIfaceName string
	if *lanMode {
		log.Println("Setting up LAN-accessible IPs...")
		info, err := network.SetupLANIPs(cfg.Devices, *lanIface)
		if err != nil {
			log.Fatalf("LAN IP setup failed: %v", err)
		}
		lanIfaceName = info.Name
	} else {
		log.Println("Setting up virtual IP aliases (local-only)...")
		if err := network.SetupIPs(cfg.Devices); err != nil {
			log.Printf("Warning: IP setup failed (run with sudo for privileged ports): %v", err)
		}
	}

	logs := accesslog.NewStore(10000)
	now := time.Now()
	for i := range cfg.Devices {
		cfg.Devices[i].StartTime = now
	}

	// Re-init configs if LAN mode changed device IPs
	if *lanMode {
		for i := range cfg.Devices {
			cfg.Devices[i].InitConfig(cfg.TmplText)
		}
	}

	// Create simulator and start all device servers
	sim := simulator.New(cfg, logs, cfg.TmplText)
	sim.StartAll()

	// Start web dashboard
	go func() {
		if err := dashboard.Serve(*dashPort, sim, logs); err != nil {
			log.Printf("Dashboard server error: %v", err)
		}
	}()

	mode := "local-only"
	if *lanMode {
		mode = "LAN (" + lanIfaceName + ")"
	}
	log.Printf("Simulator running with %d device(s) [%s]. Press Ctrl+C to stop.", len(cfg.Devices), mode)
	log.Printf("  Dashboard: http://localhost:%d", *dashPort)
	for _, dev := range cfg.Devices {
		log.Printf("  %s @ %s (HTTPS:%d, SSH:%d, SNMP:%d, TFTP:on-demand)", dev.Hostname, dev.IP, dev.HTTPSPort, dev.SSHPort, dev.SNMPPort)
	}

	// Wait for shutdown signal, then clean up IPs
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("Shutting down...")
	log.Println("Removing virtual IP aliases...")
	if *lanMode {
		network.TeardownLANIPs(sim.Devices(), lanIfaceName)
	} else {
		network.TeardownIPs(sim.Devices())
	}
	log.Println("Done.")
}
