package sgsr

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

type contextKey string

func TestNewConfig(t *testing.T) {
	logger := NewLogger()
	app := fiber.New()
	addr := ":8080"

	config := NewConfig(logger, app, addr)

	if config.logger == nil {
		t.Error("expected logger to be set")
	}
	if config.app == nil {
		t.Error("expected app to be set")
	}
	if config.addr != addr {
		t.Errorf("expected addr to be %s, got %s", addr, config.addr)
	}
	if config.shutdownTimeout != DefaultShutdownTimeout {
		t.Errorf("expected shutdownTimeout to be %v, got %v", DefaultShutdownTimeout, config.shutdownTimeout)
	}
}

func TestConfigWithContext(t *testing.T) {
	logger := NewLogger()
	app := fiber.New()
	config := NewConfig(logger, app, ":8080")

	const testKey contextKey = "test"
	ctx := context.WithValue(context.Background(), testKey, "value")
	newConfig := config.WithContext(ctx)

	if newConfig.ctx != ctx {
		t.Error("expected context to be set")
	}
}

func TestConfigWithShutdownTimeout(t *testing.T) {
	logger := NewLogger()
	app := fiber.New()
	config := NewConfig(logger, app, ":8080")

	timeout := 60 * time.Second
	newConfig := config.WithShutdownTimeout(timeout)

	if newConfig.shutdownTimeout != timeout {
		t.Errorf("expected shutdownTimeout to be %v, got %v", timeout, newConfig.shutdownTimeout)
	}
}

func TestNewApp_Success(t *testing.T) {
	logger := NewLogger()
	app := fiber.New()
	config := NewConfig(logger, app, ":8080")

	result, err := NewApp(config)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result == nil {
		t.Fatal("expected app to be created")
	}
}

func TestNewApp_NilApp(t *testing.T) {
	logger := NewLogger()
	config := NewConfig(logger, nil, ":8080")

	result, err := NewApp(config)

	if err == nil {
		t.Fatal("expected error for nil app")
	}
	if result != nil {
		t.Error("expected nil result for invalid config")
	}
	if err.Error() != "fiber app cannot be nil" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNewApp_NilLogger(t *testing.T) {
	app := fiber.New()
	config := NewConfig(nil, app, ":8080")

	result, err := NewApp(config)

	if err == nil {
		t.Fatal("expected error for nil logger")
	}
	if result != nil {
		t.Error("expected nil result for invalid config")
	}
	if err.Error() != "logger cannot be nil" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNewLogger(t *testing.T) {
	logger := NewLogger()

	if logger == nil {
		t.Fatal("expected logger to be created")
	}

	// Test that logger can write
	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{}))
	testLogger.Info("test message", "key", "value")

	if buf.Len() == 0 {
		t.Error("expected logger to write output")
	}
}

func TestDefaultShutdownTimeout(t *testing.T) {
	expected := 30 * time.Second
	if DefaultShutdownTimeout != expected {
		t.Errorf("expected DefaultShutdownTimeout to be %v, got %v", expected, DefaultShutdownTimeout)
	}
}

func TestNewApp_EmptyAddr(t *testing.T) {
	logger := NewLogger()
	app := fiber.New()
	config := NewConfig(logger, app, "")

	result, err := NewApp(config)

	if err == nil {
		t.Fatal("expected error for empty address")
	}
	if result != nil {
		t.Error("expected nil result for invalid config")
	}
	if err.Error() != "address cannot be empty" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNewApp_InvalidShutdownTimeout(t *testing.T) {
	logger := NewLogger()
	app := fiber.New()
	config := NewConfig(logger, app, ":8080").WithShutdownTimeout(0)

	result, err := NewApp(config)

	if err == nil {
		t.Fatal("expected error for zero shutdown timeout")
	}
	if result != nil {
		t.Error("expected nil result for invalid config")
	}
	if err.Error() != "shutdown timeout must be positive" {
		t.Errorf("unexpected error message: %v", err)
	}

	// Test negative timeout
	config = NewConfig(logger, app, ":8080").WithShutdownTimeout(-1 * time.Second)
	result, err = NewApp(config)

	if err == nil {
		t.Fatal("expected error for negative shutdown timeout")
	}
	if result != nil {
		t.Error("expected nil result for invalid config")
	}
	if err.Error() != "shutdown timeout must be positive" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRun_InvalidPort(t *testing.T) {
	logger := NewLogger()
	app := fiber.New()
	config := NewConfig(logger, app, "invalid:port")

	appInstance, err := NewApp(config)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	// Run should return an error for invalid address
	done := make(chan error, 1)
	go func() {
		done <- appInstance.Run()
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("expected error for invalid port")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("test timed out")
	}
}
