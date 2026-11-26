package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"static-host/internal/auth"
	"static-host/internal/config"
	"static-host/internal/httpserver"
	"static-host/internal/mcpserver"
	"static-host/internal/publication"
	"static-host/internal/store"
	"static-host/internal/web"
)

func run() error {
	args := os.Args[1:]
	cfg, err := config.Parse(args, os.LookupEnv)
	if err != nil {
		return err
	}
	db, err := store.Open(cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec("DELETE FROM sessions"); err != nil {
		return err
	}
	keyCipher, err := auth.LoadKeyCipher(cfg.DataDir, db)
	if err != nil {
		return err
	}
	publications, err := publication.New(db, cfg.DataDir, publication.Options{UploadOrigin: cfg.UploadOrigin, SiteSuffix: cfg.SiteSuffix, Local: cfg.BaseDomain == ""})
	if err != nil {
		return err
	}
	defer publications.Close()
	app, err := web.New(db, keyCipher, cfg.PanelOrigin, cfg.AdminPassword, publications)
	if err != nil {
		return err
	}
	upload := publications.UploadHandler()
	handler := httpserver.New(cfg.PanelOrigin, cfg.MCPOrigin, cfg.UploadOrigin, cfg.SiteSuffix, app, mcpserver.New(db, cfg.MCPOrigin, publications), upload, publications.SiteHandler())

	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errs := make(chan error, 1)
	go func() { errs <- server.ListenAndServe() }()

	select {
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			server.Close()
			return fmt.Errorf("shutdown: %w", err)
		}
		if err := <-errs; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
