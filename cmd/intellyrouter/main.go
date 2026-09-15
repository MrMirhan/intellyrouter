// Command intellyrouter runs the gateway, the admin API, and the dashboard.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"intellyrouter/internal/admin"
	"intellyrouter/internal/dashboard"
	"intellyrouter/internal/gateway"
	"intellyrouter/internal/ledger"
	"intellyrouter/internal/store"
	"intellyrouter/web"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "intellyrouter:", err)
		os.Exit(1)
	}
}

func run() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	addr := flag.String("addr", "127.0.0.1:7117", "listen address")
	dataDir := flag.String("data", filepath.Join(home, ".intellyrouter"), "data directory")
	resetAdmin := flag.Bool("reset-admin-token", false, "issue a new admin token and print it")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		return err
	}
	key, err := store.LoadMasterKey(*dataDir)
	if err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(*dataDir, "intellyrouter.db"), key)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	token, err := st.EnsureAdminToken(ctx, *resetAdmin)
	if err != nil {
		return err
	}
	if token != "" {
		fmt.Printf("Admin token (shown once, store it safely): %s\n", token)
	}

	client := &http.Client{}
	mux := http.NewServeMux()
	gateway.New(st, ledger.NewRecorder(st, log), client, log).Register(mux)
	admin.New(st, client, log).Register(mux)
	mux.HandleFunc("GET /api/", notFound)
	mux.HandleFunc("GET /v1/", notFound)
	mux.Handle("GET /", dashboard.Handler(web.Assets()))

	if !isLoopback(*addr) {
		log.Warn("the gateway has no TLS; keep it on localhost or put it behind a TLS proxy", "addr", *addr)
	}
	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("listening", "addr", *addr, "data", *dataDir)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, `{"error":"not found"}`)
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
