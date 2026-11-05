package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"recipeworkbench/apps/server/internal/config"
	"recipeworkbench/apps/server/internal/httpapi"
	"recipeworkbench/apps/server/internal/machine"
	"recipeworkbench/apps/server/internal/service"
	"recipeworkbench/packages/logging"
)

// OpenDatabase configures the future persistence boundary without running DDL.
// The read-only conversion service can run with no database configured.
func OpenDatabase(dsn string) (*gorm.DB, error) {
	if dsn == "" {
		return nil, nil
	}
	return gorm.Open(postgres.Open(dsn), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
}

// Serve shuts the HTTP server down when ctx is cancelled and waits for it to finish.
func Serve(ctx context.Context, listener net.Listener, handler http.Handler) error {
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			slog.Warn("server_shutdown_error", "err", err)
		}
	}()
	err := server.Serve(listener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-stopped
	return nil
}

func run(ctx context.Context) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	logConfig := cfg.Log
	logger, closer, err := logging.New(logConfig, "api", os.Stdout)
	if err != nil {
		return errors.New("cannot initialize logger: " + err.Error())
	}
	defer closer.Close()
	slog.SetDefault(logger)
	db, err := OpenDatabase(cfg.DatabaseURL)
	if err != nil {
		return errors.New("invalid database configuration: " + err.Error())
	}
	if db != nil {
		sqlDB, err := db.DB()
		if err != nil {
			return errors.New("cannot initialize database pool")
		}
		defer sqlDB.Close()
	}
	reader := machine.New(cfg.MockURL, cfg.Timeout, logger)
	router, err := httpapi.NewRouter(service.New(reader, logger), logger)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		return err
	}
	logger.Info("server_started", "address", "http://"+cfg.Address, "mock_url", cfg.MockURL,
		"log_level", logConfig.Level.String(), "log_dest", logConfig.Dest, "log_dir", logConfig.Dir,
		"database", cfg.DatabaseURL != "")
	return Serve(ctx, listener, router)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("server_exit", "err", err)
		os.Exit(1)
	}
}
