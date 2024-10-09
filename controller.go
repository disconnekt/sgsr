package sgsr

import (
	"context"
	"log/slog"
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

func NewConfig(l *slog.Logger, app *fiber.App, addr string) Config {
	return Config{
		ctx:    context.Background(),
		logger: l,
		app:    app,
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
		a.cfg.logger.Info("Trying to shut down gracefully")

		go func() {
			time.Sleep(time.Second * 30)
			a.cfg.logger.Error("Exit by shut down timeout")
			os.Exit(3)
		}()

		_ = a.cfg.app.Shutdown()
	}()

	a.cfg.logger.Info("Status", "Listening addr", a.cfg.addr)

	if err := a.cfg.app.Listen(a.cfg.addr); err != nil {
		a.cfg.logger.Error(err.Error())
		panic(err)
	}
}
