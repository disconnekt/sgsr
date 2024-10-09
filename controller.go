package sgsr

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
)

type Config struct {
	app    *fiber.App
	logger *slog.Logger
	ctx    context.Context
	addr   string
}

func NewConfig(l *slog.Logger, a *fiber.App, addr string) Config {
	return Config{
		ctx:    context.Background(),
		logger: l,
		app:    a,
		addr:   addr,
	}
}

func (c Config) WithContext(ctx context.Context) Config {
	c.ctx = ctx
	return c
}

type App struct {
	cfg Config
}

func NewApp(config Config) *App {
	return &App{cfg: config}
}

func NewLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{}))
}

func (a App) Run() {
	ctx, stop := signal.NotifyContext(a.cfg.ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		stop()
		a.cfg.logger.Info("trying shut down gracefully")

		go func() {
			time.Sleep(time.Second * 30)
			a.cfg.logger.Error("exit by shut down timeout")
			os.Exit(3)
		}()

		timeoutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		_ = a.cfg.app.ShutdownWithContext(timeoutCtx)
	}()

	a.cfg.logger.Info("status", "listening addr", a.cfg.addr)

	if err := a.cfg.app.Listen(a.cfg.addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
		a.cfg.logger.Error(err.Error())
		panic(err)
	}
}
