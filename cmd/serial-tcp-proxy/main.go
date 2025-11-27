package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/hoon-ch/serial-tcp-proxy/internal/config"
	"github.com/hoon-ch/serial-tcp-proxy/internal/logger"
	"github.com/hoon-ch/serial-tcp-proxy/internal/proxy"
)

var Version = "dev"

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		println("Configuration error:", err.Error())
		os.Exit(1)
	}

	// Initialize logger
	log, err := logger.New(cfg.LogPackets, cfg.LogFile)
	if err != nil {
		println("Logger error:", err.Error())
		os.Exit(1)
	}

	log.Info("Starting Serial TCP Proxy v%s", Version)
	log.Info("Upstream: %s", cfg.UpstreamAddr())
	log.Info("Listen: %s", cfg.ListenAddr())
	log.Info("Max clients: %d", cfg.MaxClients)
	log.Info("Packet logging: %v", cfg.LogPackets)

	// Create and start proxy server
	server := proxy.NewServer(cfg, log)

	if err := server.Start(); err != nil {
		log.Error("Failed to start proxy: %v", err)
		os.Exit(1)
	}

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	log.Info("Received signal %v, shutting down...", sig)

	// Graceful shutdown
	server.Stop()
}
