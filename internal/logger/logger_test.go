package logger

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestNew_NoPacketLogging(t *testing.T) {
	logger, err := New(false, "")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer logger.Close()

	if logger.logPackets != false {
		t.Error("Expected logPackets=false")
	}
}

func TestNew_WithPacketLogging(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test_packets_*.log")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	logger, err := New(true, tmpFile.Name())
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer logger.Close()

	if logger.logPackets != true {
		t.Error("Expected logPackets=true")
	}

	if logger.file == nil {
		t.Error("Expected file to be opened")
	}
}

func TestLogger_Info(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		stdWriter:  &buf,
		logPackets: false,
	}

	logger.Info("Test message %d", 123)

	output := buf.String()
	if !strings.Contains(output, "[INFO]") {
		t.Errorf("Expected [INFO] in output, got: %s", output)
	}
	if !strings.Contains(output, "Test message 123") {
		t.Errorf("Expected 'Test message 123' in output, got: %s", output)
	}
}

func TestLogger_Warn(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		stdWriter:  &buf,
		logPackets: false,
	}

	logger.Warn("Warning message")

	output := buf.String()
	if !strings.Contains(output, "[WARN]") {
		t.Errorf("Expected [WARN] in output, got: %s", output)
	}
}

func TestLogger_Error(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		stdWriter:  &buf,
		logPackets: false,
	}

	logger.Error("Error message")

	output := buf.String()
	if !strings.Contains(output, "[ERROR]") {
		t.Errorf("Expected [ERROR] in output, got: %s", output)
	}
}

func TestLogger_LogPacket_Disabled(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		stdWriter:  &buf,
		logPackets: false,
	}

	logger.LogPacket("UP→", []byte{0xf7, 0x0e}, "")

	if buf.Len() > 0 {
		t.Errorf("Expected no output when logging disabled, got: %s", buf.String())
	}
}

func TestLogger_LogPacket_Enabled(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		stdWriter:  &buf,
		logPackets: true,
	}

	logger.LogPacket("UP→", []byte{0xf7, 0x0e, 0x1f}, "")

	output := buf.String()
	if !strings.Contains(output, "[PKT]") {
		t.Errorf("Expected [PKT] in output, got: %s", output)
	}
	if !strings.Contains(output, "[UP→]") {
		t.Errorf("Expected [UP→] in output, got: %s", output)
	}
	if !strings.Contains(output, "f7 0e 1f") {
		t.Errorf("Expected 'f7 0e 1f' in output, got: %s", output)
	}
	if !strings.Contains(output, "(3 bytes)") {
		t.Errorf("Expected '(3 bytes)' in output, got: %s", output)
	}
}

func TestLogger_LogPacket_WithSource(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		stdWriter:  &buf,
		logPackets: true,
	}

	logger.LogPacket("→UP", []byte{0xf7, 0x0e}, "client#1")

	output := buf.String()
	if !strings.Contains(output, "from client#1") {
		t.Errorf("Expected 'from client#1' in output, got: %s", output)
	}
}

func TestLogger_LogPacket_HexFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		stdWriter:  &buf,
		logPackets: true,
	}

	logger.LogPacket("UP→", []byte{0x00, 0xff, 0xab, 0xcd}, "")

	output := buf.String()
	if !strings.Contains(output, "00 ff ab cd") {
		t.Errorf("Expected '00 ff ab cd' in output, got: %s", output)
	}
}

func TestLogger_SetOutput(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	logger := &Logger{
		stdWriter:  &buf1,
		logPackets: false,
	}

	logger.Info("First message")
	logger.SetOutput(&buf2)
	logger.Info("Second message")

	if !strings.Contains(buf1.String(), "First message") {
		t.Error("Expected 'First message' in buf1")
	}
	if !strings.Contains(buf2.String(), "Second message") {
		t.Error("Expected 'Second message' in buf2")
	}
}

func TestLogger_IsPacketLoggingEnabled(t *testing.T) {
	logger := &Logger{logPackets: true}
	if !logger.IsPacketLoggingEnabled() {
		t.Error("Expected IsPacketLoggingEnabled=true")
	}

	logger.logPackets = false
	if logger.IsPacketLoggingEnabled() {
		t.Error("Expected IsPacketLoggingEnabled=false")
	}
}
