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
)

type Config struct {
	h      http.Handler
	logger *slog.Logger
	addr   string
	ctx    context.Context
}

func NewConfig() Config {
	return Config{
		h:      http.DefaultServeMux,
		addr:   ":8080",
		ctx:    context.Background(),
		logger: NewLogger(),
	}
}

type Option func(*Config)

func (c Config) WithLogger(logger *slog.Logger) Config {
	c.logger = logger
	return c
}

func (c Config) WithHandler(h http.Handler) Config {
	c.h = h
	return c
}

func (c Config) WithAddr(addr string) Config {
	c.addr = addr
	return c
}

func (c Config) WithContext(ctx context.Context) Config {
	c.ctx = ctx
	return c
}

type App struct {
	cfg Config
}

func NewApp(config Config, o ...Option) *App {
	for _, opt := range o {
		opt(&config)
	}

	return &App{cfg: config}
}

func NewLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{}))
}

func (a App) Run() {
	ctx, stop := signal.NotifyContext(a.cfg.ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := http.Server{
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
		Addr:         a.cfg.addr,
		Handler:      a.cfg.h,
	}

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

		_ = srv.Shutdown(timeoutCtx) //nolint:contextcheck
	}()

	a.cfg.logger.Info("status", "listening", "addr", a.cfg.addr)

	err := srv.ListenAndServe()
	if !errors.Is(err, http.ErrServerClosed) {
		a.cfg.logger.Error(err.Error())

		panic(err)
	}
}
