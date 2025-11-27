package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	UpstreamHost   string        `json:"upstream_host"`
	UpstreamPort   int           `json:"upstream_port"`
	ListenPort     int           `json:"listen_port"`
	MaxClients     int           `json:"max_clients"`
	LogPackets     bool          `json:"log_packets"`
	LogFile        string        `json:"log_file"`
	WebPort        int           `json:"web_port"`
	ReconnectDelay time.Duration `json:"-"`
}

func Load() (*Config, error) {
	config := &Config{
		UpstreamPort:   8899,
		ListenPort:     18899,
		MaxClients:     10,
		LogPackets:     false,
		LogFile:        "/data/packets.log",
		WebPort:        18080,
		ReconnectDelay: time.Second,
	}

	// Try to load from Home Assistant options file first
	if optionsData, err := os.ReadFile("/data/options.json"); err == nil {
		if err := json.Unmarshal(optionsData, config); err != nil {
			return nil, fmt.Errorf("failed to parse options.json: %w", err)
		}
	}

	// Environment variables override file config
	if host := os.Getenv("UPSTREAM_HOST"); host != "" {
		config.UpstreamHost = host
	}

	if port := os.Getenv("UPSTREAM_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			config.UpstreamPort = p
		}
	}

	if port := os.Getenv("LISTEN_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			config.ListenPort = p
		}
	}

	if maxClients := os.Getenv("MAX_CLIENTS"); maxClients != "" {
		if m, err := strconv.Atoi(maxClients); err == nil {
			config.MaxClients = m
		}
	}

	if logPackets := os.Getenv("LOG_PACKETS"); logPackets != "" {
		config.LogPackets = logPackets == "true" || logPackets == "1"
	}

	if logFile := os.Getenv("LOG_FILE"); logFile != "" {
		config.LogFile = logFile
	}

	if webPort := os.Getenv("WEB_PORT"); webPort != "" {
		if p, err := strconv.Atoi(webPort); err == nil {
			config.WebPort = p
		}
	}

	// Validate required fields
	if config.UpstreamHost == "" {
		return nil, fmt.Errorf("UPSTREAM_HOST is required")
	}

	if config.UpstreamPort <= 0 || config.UpstreamPort > 65535 {
		return nil, fmt.Errorf("invalid UPSTREAM_PORT: %d", config.UpstreamPort)
	}

	if config.ListenPort <= 0 || config.ListenPort > 65535 {
		return nil, fmt.Errorf("invalid LISTEN_PORT: %d", config.ListenPort)
	}

	if config.MaxClients <= 0 || config.MaxClients > 100 {
		return nil, fmt.Errorf("MAX_CLIENTS must be between 1 and 100")
	}

	return config, nil
}

func (c *Config) UpstreamAddr() string {
	return fmt.Sprintf("%s:%d", c.UpstreamHost, c.UpstreamPort)
}

func (c *Config) ListenAddr() string {
	return fmt.Sprintf(":%d", c.ListenPort)
}
