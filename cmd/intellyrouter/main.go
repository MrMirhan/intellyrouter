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

	"github.com/MrMirhan/intellyrouter/internal/admin"
	"github.com/MrMirhan/intellyrouter/internal/dashboard"
	"github.com/MrMirhan/intellyrouter/internal/eval"
	"github.com/MrMirhan/intellyrouter/internal/gateway"
	"github.com/MrMirhan/intellyrouter/internal/ledger"
	"github.com/MrMirhan/intellyrouter/internal/store"
	"github.com/MrMirhan/intellyrouter/web"
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
	evalTasks := flag.String("eval-tasks", "eval/tasks", "directory with eval tasks for the dashboard")
	claude := flag.String("claude", "claude", "Claude Code binary for eval runs and for directors that answer through Claude Code")
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

	if err := st.FailInterruptedEvalRuns(ctx); err != nil {
		return err
	}
	evals := eval.NewManager(st, *evalTasks, localURL(*addr), *claude, log)

	pruned := make(chan struct{})
	go func() {
		defer close(pruned)
		pruneContent(ctx, st, log)
	}()
	// The pruner must stop before the store closes.
	defer func() {
		stop()
		<-pruned
	}()

	client := &http.Client{}
	mux := http.NewServeMux()
	gw := gateway.New(st, ledger.NewRecorder(st, log), client, log)
	gw.UseClaudeCode(*claude, filepath.Join(*dataDir, "claude-director"))
	gw.Register(mux)
	admin.New(st, client, log, evals, *claude).Register(mux)
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
	evals.Shutdown()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

// pruneContent deletes captured content older than the retention setting once
// at startup and then every hour until ctx ends.
func pruneContent(ctx context.Context, st *store.Store, log *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		_, days, err := st.CaptureSettings(ctx)
		if err == nil {
			err = st.PruneContent(ctx, time.Now().AddDate(0, 0, -days).UnixMilli())
		}
		if err != nil && ctx.Err() == nil {
			log.Warn("prune captured content", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, `{"error":"not found"}`)
}

// localURL is the address that Claude Code, started by eval runs on this
// machine, uses to reach the gateway.
func localURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
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
