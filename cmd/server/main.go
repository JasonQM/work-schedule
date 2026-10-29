package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/JasonQM/work-schedule/internal/platform"
	"github.com/joho/godotenv"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg := platform.Config{PublicURL: env("PUBLIC_URL", "http://localhost:8080"), WebDir: env("WEB_DIR", "web/dist"), Secure: env("COOKIE_SECURE", "true") == "true"}
	if err := platform.ValidateConfig(cfg); err != nil {
		return err
	}
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	store, err := platform.Open(startup, os.Getenv("MYSQL_DSN"))
	if err != nil {
		return err
	}
	defer store.DB.Close()
	if err = store.Migrate(startup); err != nil {
		return err
	}
	if err = store.Bootstrap(startup, os.Getenv("BOOTSTRAP_EMAIL"), os.Getenv("BOOTSTRAP_PASSWORD")); err != nil {
		return err
	}
	app := platform.NewApp(store, cfg)
	srv := &http.Server{Addr: env("LISTEN_ADDR", "127.0.0.1:8080"), Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	srv.BaseContext = func(net.Listener) context.Context { return ctx }
	listener, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return err
	}
	errs := make(chan error, 1)
	go func() { slog.Info("server listening", "address", srv.Addr); errs <- srv.Serve(listener) }()
	select {
	case err = <-errs:
		if err != http.ErrServerClosed {
			return err
		}
	case <-ctx.Done():
		shutdown, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		return srv.Shutdown(shutdown)
	}
	return nil
}
