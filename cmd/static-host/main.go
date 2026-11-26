package main

import (
	"context"
	"errors"
	"fmt"
	"net"
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
	publications, err := publication.New(db, cfg.DataDir, publication.Options{
		UploadOrigin:        cfg.UploadOrigin,
		SiteSuffix:          cfg.SiteSuffix,
		Local:               cfg.BaseDomain == "",
		BaseDomain:          cfg.BaseDomain,
		DomainIngressIPs:    cfg.DomainIngressIPs,
		DomainReservedHosts: cfg.DomainReservedHosts,
	})
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
	servers := []*http.Server{server}
	var askListener net.Listener
	if len(cfg.DomainIngressIPs) > 0 {
		askAddress := "0.0.0.0:8082"
		if cfg.BaseDomain == "" {
			askAddress = "127.0.0.1:8082"
		}
		askListener, err = net.Listen("tcp", askAddress)
		if err != nil {
			return fmt.Errorf("domain ask listener: %w", err)
		}
		defer askListener.Close()
		servers = append(servers, &http.Server{
			Addr:              askAddress,
			Handler:           publications.DomainAskHandler(),
			ReadHeaderTimeout: 5 * time.Second,
		})
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errs := make(chan error, len(servers))
	go func() { errs <- server.ListenAndServe() }()
	if askListener != nil {
		askServer := servers[1]
		go func() { errs <- askServer.Serve(askListener) }()
	}

	completed := 0
	var runErr error
	select {
	case err := <-errs:
		completed++
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = err
		}
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, srv := range servers {
		if err := srv.Shutdown(shutdownCtx); err != nil {
			if runErr == nil {
				runErr = fmt.Errorf("shutdown: %w", err)
			}
			_ = srv.Close()
		}
	}
	for completed < len(servers) {
		err := <-errs
		completed++
		if !errors.Is(err, http.ErrServerClosed) && runErr == nil {
			runErr = err
		}
	}
	return runErr
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
