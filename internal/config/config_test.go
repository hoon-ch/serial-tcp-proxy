package config

import (
	"os"
	"testing"
)

func TestLoad_RequiredFields(t *testing.T) {
	os.Clearenv()

	_, err := Load()
	if err == nil {
		t.Error("Expected error when UPSTREAM_HOST is not set")
	}
}

func TestLoad_DefaultValues(t *testing.T) {
	os.Clearenv()
	os.Setenv("UPSTREAM_HOST", "192.168.1.100")

	config, err := Load()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if config.UpstreamHost != "192.168.1.100" {
		t.Errorf("Expected UpstreamHost=192.168.1.100, got %s", config.UpstreamHost)
	}

	if config.UpstreamPort != 8899 {
		t.Errorf("Expected UpstreamPort=8899, got %d", config.UpstreamPort)
	}

	if config.ListenPort != 18899 {
		t.Errorf("Expected ListenPort=18899, got %d", config.ListenPort)
	}

	if config.MaxClients != 10 {
		t.Errorf("Expected MaxClients=10, got %d", config.MaxClients)
	}

	if config.LogPackets != false {
		t.Errorf("Expected LogPackets=false, got %v", config.LogPackets)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	os.Clearenv()
	os.Setenv("UPSTREAM_HOST", "10.0.0.1")
	os.Setenv("UPSTREAM_PORT", "9999")
	os.Setenv("LISTEN_PORT", "19999")
	os.Setenv("MAX_CLIENTS", "20")
	os.Setenv("LOG_PACKETS", "true")
	os.Setenv("LOG_FILE", "/tmp/test.log")

	config, err := Load()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if config.UpstreamHost != "10.0.0.1" {
		t.Errorf("Expected UpstreamHost=10.0.0.1, got %s", config.UpstreamHost)
	}

	if config.UpstreamPort != 9999 {
		t.Errorf("Expected UpstreamPort=9999, got %d", config.UpstreamPort)
	}

	if config.ListenPort != 19999 {
		t.Errorf("Expected ListenPort=19999, got %d", config.ListenPort)
	}

	if config.MaxClients != 20 {
		t.Errorf("Expected MaxClients=20, got %d", config.MaxClients)
	}

	if config.LogPackets != true {
		t.Errorf("Expected LogPackets=true, got %v", config.LogPackets)
	}

	if config.LogFile != "/tmp/test.log" {
		t.Errorf("Expected LogFile=/tmp/test.log, got %s", config.LogFile)
	}
}

func TestLoad_InvalidPort(t *testing.T) {
	os.Clearenv()
	os.Setenv("UPSTREAM_HOST", "192.168.1.100")
	os.Setenv("UPSTREAM_PORT", "99999")

	_, err := Load()
	if err == nil {
		t.Error("Expected error for invalid port")
	}
}

func TestLoad_InvalidMaxClients(t *testing.T) {
	os.Clearenv()
	os.Setenv("UPSTREAM_HOST", "192.168.1.100")
	os.Setenv("MAX_CLIENTS", "0")

	_, err := Load()
	if err == nil {
		t.Error("Expected error for invalid max_clients=0")
	}

	os.Setenv("MAX_CLIENTS", "101")
	_, err = Load()
	if err == nil {
		t.Error("Expected error for invalid max_clients=101")
	}
}

func TestConfig_UpstreamAddr(t *testing.T) {
	config := &Config{
		UpstreamHost: "192.168.1.100",
		UpstreamPort: 8899,
	}

	expected := "192.168.1.100:8899"
	if config.UpstreamAddr() != expected {
		t.Errorf("Expected %s, got %s", expected, config.UpstreamAddr())
	}
}

func TestConfig_ListenAddr(t *testing.T) {
	config := &Config{
		ListenPort: 18899,
	}

	expected := ":18899"
	if config.ListenAddr() != expected {
		t.Errorf("Expected %s, got %s", expected, config.ListenAddr())
	}
}
