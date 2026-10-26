package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"secunda/internal/app"
	"secunda/internal/cache"
	"secunda/internal/config"
	"secunda/internal/database"
	"secunda/internal/httpapi"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	switch command {
	case "serve":
		return serve()
	case "migrate":
		return migrate(logger)
	case "healthcheck":
		return healthcheck()
	default:
		return fmt.Errorf("unknown command %q; use serve, migrate or healthcheck", command)
	}
}

func migrate(logger *slog.Logger) error {
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		return fmt.Errorf("MYSQL_DSN is required")
	}

	db, err := database.Open(dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := database.Migrate(ctx, db); err != nil {
		return err
	}

	logger.Info("migrations applied")
	return nil
}

func serve() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	db, err := database.Open(cfg.MySQLDSN)
	if err != nil {
		return err
	}
	defer db.Close()

	startup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = db.PingContext(startup)
	cancel()
	if err != nil {
		return fmt.Errorf("connect to MySQL: %w", err)
	}

	auth, err := app.NewAuth(cfg.JWTSecret, cfg.JWTTTL)
	if err != nil {
		return err
	}

	taskCache := cache.New(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, logger)
	defer taskCache.Close()

	service := app.New(db, taskCache, auth)
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.New(service, auth, db, logger, cfg.RateLimit, cfg.AuthRateLimit),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       time.Minute,
		MaxHeaderBytes:    16 << 10,
	}

	return runHTTPServer(server, logger)
}

func runHTTPServer(server *http.Server, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	result := make(chan error, 1)
	go func() {
		result <- server.ListenAndServe()
	}()
	logger.Info("http server started", "address", server.Addr)

	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	}
}

func healthcheck() error {
	address := os.Getenv("HTTP_ADDR")
	if address == "" {
		address = ":8080"
	}

	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}

	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: HTTP %d", response.StatusCode)
	}

	return nil
}
