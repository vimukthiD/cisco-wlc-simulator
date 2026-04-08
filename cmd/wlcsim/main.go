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
	"github.com/vimukthi/cisco-wlc-sim/internal/restconf"
	"github.com/vimukthi/cisco-wlc-sim/internal/snmp"
	"github.com/vimukthi/cisco-wlc-sim/internal/sshsim"
)

func main() {
	configPath := flag.String("config", "configs/devices.yaml", "path to devices config file")
	dashPort := flag.Int("dashboard-port", 8080, "web dashboard port")
	setupIPs := flag.Bool("setup-ips", false, "add virtual IP aliases (requires root/sudo)")
	teardownIPs := flag.Bool("teardown-ips", false, "remove virtual IP aliases (requires root/sudo)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if *teardownIPs {
		log.Println("Removing virtual IP aliases...")
		network.TeardownIPs(cfg.Devices)
		log.Println("Done.")
		return
	}

	if *setupIPs {
		log.Println("Setting up virtual IP aliases...")
		if err := network.SetupIPs(cfg.Devices); err != nil {
			log.Fatalf("Failed to setup IPs: %v", err)
		}
		log.Println("Virtual IPs configured.")
	}

	logs := accesslog.NewStore(10000)
	now := time.Now()
	for i := range cfg.Devices {
		cfg.Devices[i].StartTime = now
	}

	// Start RESTCONF HTTPS servers (one per device)
	for i := range cfg.Devices {
		dev := &cfg.Devices[i]
		go func() {
			if err := restconf.Serve(dev, cfg.Auth, logs); err != nil {
				log.Printf("[%s] RESTCONF server error: %v", dev.Hostname, err)
			}
		}()
	}

	// Start SSH servers (one per device)
	for i := range cfg.Devices {
		dev := &cfg.Devices[i]
		go func() {
			if err := sshsim.Serve(dev, cfg.Auth, logs); err != nil {
				log.Printf("[%s] SSH server error: %v", dev.Hostname, err)
			}
		}()
	}

	// Start SNMP agents (one per device)
	for i := range cfg.Devices {
		dev := &cfg.Devices[i]
		go func() {
			if err := snmp.Serve(dev, cfg.Auth, logs); err != nil {
				log.Printf("[%s] SNMP agent error: %v", dev.Hostname, err)
			}
		}()
	}

	// Start web dashboard
	go func() {
		if err := dashboard.Serve(*dashPort, cfg, logs); err != nil {
			log.Printf("Dashboard server error: %v", err)
		}
	}()

	log.Printf("Simulator running with %d device(s). Press Ctrl+C to stop.", len(cfg.Devices))
	log.Printf("  Dashboard: http://localhost:%d", *dashPort)
	for _, dev := range cfg.Devices {
		log.Printf("  %s @ %s (HTTPS:%d, SSH:%d, SNMP:%d)", dev.Hostname, dev.IP, dev.HTTPSPort, dev.SSHPort, dev.SNMPPort)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("Shutting down...")
}
