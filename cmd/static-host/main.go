package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"static-host/internal/auth"
	"static-host/internal/mcpserver"
	"static-host/internal/store"
	"static-host/internal/web"
)

func run() error {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "owner" {
		return owner(args[1:])
	}
	flags := flag.NewFlagSet("static-host", flag.ContinueOnError)
	dir := flags.String("data-dir", "./data", "data directory")
	if err := flags.Parse(args); err != nil { return err }
	if flags.NArg() != 0 { return errors.New("unexpected arguments") }
	db, err := store.Open(*dir)
	if err != nil { return err }
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil { return err }
	if count == 0 { return errors.New("owner not initialized; run owner init") }
	if _, err := db.Exec("DELETE FROM sessions WHERE expires_at <= ?", time.Now().Unix()); err != nil { return err }
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, "ok\n")
	})
	app, err := web.New(db)
	if err != nil { return err }
	mux.Handle("/mcp", mcpserver.New(db))
	mux.Handle("/", app)

	server := &http.Server{
		Addr:              "127.0.0.1:8080",
		Handler:           http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { if r.Host != "127.0.0.1:8080" { http.Error(w,"Forbidden",http.StatusForbidden); return }; mux.ServeHTTP(w,r) }),
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

func owner(args []string) error {
	if len(args) == 0 || (args[0] != "init" && args[0] != "reset-password") { return errors.New("usage: owner init|reset-password") }
	command := args[0]
	flags := flag.NewFlagSet("owner "+command, flag.ContinueOnError)
	dir := flags.String("data-dir", "./data", "data directory")
	login := flags.String("login", "", "owner login")
	if err := flags.Parse(args[1:]); err != nil { return err }
	if flags.NArg() != 0 || (command == "reset-password" && *login != "") { return errors.New("unexpected arguments") }
	if command == "init" { if _, err := auth.Login(*login); err != nil { return err } }
	password, err := auth.ReadPassword(os.Stdin)
	if err != nil { return err }
	db, err := store.Open(*dir)
	if err != nil { return err }
	defer db.Close()
	if command == "init" { err = auth.Init(db, *login, password) } else { err = auth.Reset(db, password) }
	if err != nil { return err }
	fmt.Println("Owner credentials updated")
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
