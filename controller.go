package sgsr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
)

// DefaultShutdownTimeout is the default time allowed for graceful shutdown.
const DefaultShutdownTimeout = 30 * time.Second

// Sentinel errors returned by NewApp and Run.
var (
	ErrNilApp         = errors.New("fiber app cannot be nil")
	ErrNilLogger      = errors.New("logger cannot be nil")
	ErrNilContext     = errors.New("context cannot be nil")
	ErrNilAppReceiver = errors.New("app cannot be nil")
	ErrEmptyAddr      = errors.New("address cannot be empty")
	ErrInvalidTimeout = errors.New("shutdown timeout must be positive")
)

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
// Returns an error if the config is invalid (nil app, nil logger, nil context, empty addr, or invalid timeout).
func NewApp(config Config) (*App, error) {
	if config.app == nil {
		return nil, ErrNilApp
	}
	if config.logger == nil {
		return nil, ErrNilLogger
	}
	if config.ctx == nil {
		return nil, ErrNilContext
	}
	config.addr = strings.TrimSpace(config.addr)
	if config.addr == "" {
		return nil, ErrEmptyAddr
	}
	if config.shutdownTimeout <= 0 {
		return nil, ErrInvalidTimeout
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
	if a == nil {
		return ErrNilAppReceiver
	}

	ctx, stop := signal.NotifyContext(a.cfg.ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := ctx.Err(); err != nil {
		return err
	}

	// errChan receives the result of Listen() — nil on clean shutdown, error otherwise.
	// Buffered so the goroutine never blocks regardless of which select branch runs first.
	errChan := make(chan error, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				errChan <- fmt.Errorf("server panic: %v", r)
			}
		}()
		a.cfg.logger.Info("server listening", "addr", a.cfg.addr)
		errChan <- a.cfg.app.Listen(a.cfg.addr)
	}()

	select {
	case <-ctx.Done():
		stop()
		a.cfg.logger.Info("trying shut down gracefully")

		// Use context.Background() for shutdown: the parent context is already done,
		// so we need a fresh context with timeout.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.shutdownTimeout)
		defer cancel()

		if err := a.cfg.app.ShutdownWithContext(shutdownCtx); err != nil {
			a.cfg.logger.Error("shutdown error", "error", err)
			select {
			case listenErr := <-errChan:
				if listenErr != nil {
					a.cfg.logger.Error("server error", "error", listenErr)
					return listenErr
				}
			default:
			}
			return err
		}

		select {
		case listenErr := <-errChan:
			if listenErr != nil {
				a.cfg.logger.Error("server error", "error", listenErr)
				return listenErr
			}
		case <-shutdownCtx.Done():
			return fmt.Errorf("timed out waiting for server to stop: %w", shutdownCtx.Err())
		}

		return nil

	case err := <-errChan:
		if err != nil {
			a.cfg.logger.Error("server error", "error", err)
		}
		return err
	}
}
