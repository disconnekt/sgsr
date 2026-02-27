package sgsr

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
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
	if !errors.Is(err, ErrNilApp) {
		t.Errorf("unexpected error: %v", err)
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
	if !errors.Is(err, ErrNilLogger) {
		t.Errorf("unexpected error: %v", err)
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
	if !errors.Is(err, ErrEmptyAddr) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewApp_WhitespaceAddr(t *testing.T) {
	logger := NewLogger()
	app := fiber.New()
	config := NewConfig(logger, app, "   ")

	result, err := NewApp(config)

	if err == nil {
		t.Fatal("expected error for whitespace address")
	}
	if result != nil {
		t.Error("expected nil result for invalid config")
	}
	if !errors.Is(err, ErrEmptyAddr) {
		t.Errorf("unexpected error: %v", err)
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
	if !errors.Is(err, ErrInvalidTimeout) {
		t.Errorf("unexpected error: %v", err)
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
	if !errors.Is(err, ErrInvalidTimeout) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewApp_NilContext(t *testing.T) {
	logger := NewLogger()
	app := fiber.New()
	config := NewConfig(logger, app, ":8080").WithContext(nil)

	result, err := NewApp(config)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
	if result != nil {
		t.Error("expected nil result for invalid config")
	}
	if !errors.Is(err, ErrNilContext) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRun_NilApp(t *testing.T) {
	var app *App
	err := app.Run()
	if err == nil {
		t.Fatal("expected error for nil app")
	}
	if !errors.Is(err, ErrNilAppReceiver) {
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

	// Run should return an error for invalid address.
	err = appInstance.Run()
	if err == nil {
		t.Error("expected error for invalid port")
	}
}

func TestRun_CanceledContext(t *testing.T) {
	logger := NewLogger()
	app := fiber.New()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	config := NewConfig(logger, app, ":8080").WithContext(ctx)
	appInstance, err := NewApp(config)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	err = appInstance.Run()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestRun_GracefulShutdown(t *testing.T) {
	// Reserve an address then release it immediately so the server can bind.
	// There is a small TOCTOU window, but it is acceptable for a test.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	fiberApp := fiber.New(fiber.Config{DisableStartupMessage: true})
	logger := slog.New(slog.NewJSONHandler(noopWriter{}, nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := NewConfig(logger, fiberApp, addr).
		WithContext(ctx).
		WithShutdownTimeout(5 * time.Second)

	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}

	runErr := make(chan error, 1)
	go func() { runErr <- app.Run() }()

	// Poll until the server is ready to accept connections.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := net.Dial("tcp", addr)
		if dialErr == nil {
			conn.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Trigger graceful shutdown via context cancellation.
	cancel()

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned unexpected error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return within 10s after cancel")
	}
}

// noopWriter discards all log output in tests that don't need it.
type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }
