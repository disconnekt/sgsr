package sgsr

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
)

// DefaultShutdownTimeout is the default time allowed for graceful shutdown.
const DefaultShutdownTimeout = 30 * time.Second

// Config holds the configuration for the application.
type Config struct {
	app             *fiber.App
	logger          *slog.Logger
	ctx             context.Context
	addr            string
	shutdownTimeout time.Duration
}

// NewConfig creates a new Config with the provided logger, Fiber app, and address.
// The shutdown timeout defaults to DefaultShutdownTimeout.
func NewConfig(l *slog.Logger, app *fiber.App, addr string) Config {
	return Config{
		ctx:             context.Background(),
		logger:          l,
		app:             app,
		addr:            addr,
		shutdownTimeout: DefaultShutdownTimeout,
	}
}

// WithContext returns a new Config with the specified context.
func (c Config) WithContext(ctx context.Context) Config {
	c.ctx = ctx
	return c
}

// WithShutdownTimeout returns a new Config with the specified shutdown timeout duration.
func (c Config) WithShutdownTimeout(timeout time.Duration) Config {
	c.shutdownTimeout = timeout
	return c
}

// App represents the application with graceful shutdown capabilities.
type App struct {
	cfg Config
}

// NewApp creates a new App with the provided config.
// Returns an error if the config is invalid (nil app, nil logger, empty addr, or invalid timeout).
func NewApp(config Config) (*App, error) {
	if config.app == nil {
		return nil, errors.New("fiber app cannot be nil")
	}
	if config.logger == nil {
		return nil, errors.New("logger cannot be nil")
	}
	if config.addr == "" {
		return nil, errors.New("address cannot be empty")
	}
	if config.shutdownTimeout <= 0 {
		return nil, errors.New("shutdown timeout must be positive")
	}
	return &App{cfg: config}, nil
}

// NewLogger creates a new structured logger that writes JSON to stderr.
func NewLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{}))
}

// Run starts the server and blocks until a shutdown signal is received.
// Returns an error if the server fails to start or encounters an error during shutdown.
func (a *App) Run() error {
	ctx, stop := signal.NotifyContext(a.cfg.ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Channel to communicate Listen errors
	errChan := make(chan error, 1)

	// Start server in goroutine
	go func() {
		a.cfg.logger.Info("server listening", "addr", a.cfg.addr)
		if err := a.cfg.app.Listen(a.cfg.addr); err != nil {
			a.cfg.logger.Error("server error", "error", err.Error())
			errChan <- err
		}
	}()

	// Wait for either shutdown signal or server error
	select {
	case <-ctx.Done():
		stop()
		a.cfg.logger.Info("trying shut down gracefully")

		// Use context.Background() for shutdown to ensure it's not cancelled prematurely
		// The parent context is already done, so we need a fresh context with timeout
		shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.shutdownTimeout)
		defer cancel()

		if err := a.cfg.app.ShutdownWithContext(shutdownCtx); err != nil {
			a.cfg.logger.Error("shutdown error", "error", err.Error())
			return err
		}
		return nil

	case err := <-errChan:
		return err
	}
}
