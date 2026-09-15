// Command intelly-eval compares routes by running Claude Code on the eval
// tasks through the gateway and grading each result with the task's tests.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"intellyrouter/internal/eval"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "intelly-eval:", err)
		os.Exit(1)
	}
}

func run() error {
	gateway := flag.String("gateway", "http://127.0.0.1:7117", "gateway base URL")
	key := flag.String("key", os.Getenv("INTELLY_GATEWAY_KEY"), "gateway key (default $INTELLY_GATEWAY_KEY)")
	adminToken := flag.String("admin-token", os.Getenv("INTELLY_ADMIN_TOKEN"), "admin token for reading the ledger (default $INTELLY_ADMIN_TOKEN)")
	routes := flag.String("routes", "", "comma-separated routes to compare (required)")
	tasksDir := flag.String("tasks", "eval/tasks", "task directory")
	only := flag.String("only", "", "comma-separated task IDs to run (default all)")
	mode := flag.String("mode", string(eval.ModeKey), `"key": gateway key in bare mode; "subscription": Claude login plus x-intelly-key`)
	parallel := flag.Int("parallel", 1, "number of runs at the same time")
	out := flag.String("out", "eval-results.json", "file for the full results")
	claude := flag.String("claude", "claude", "Claude Code binary")
	yes := flag.Bool("yes", false, "start the runs; without this flag only the plan is printed")
	flag.Parse()

	routeList := splitList(*routes)
	switch {
	case len(routeList) == 0:
		return errors.New("-routes is required")
	case *key == "" || *adminToken == "":
		return errors.New("-key and -admin-token are required")
	case eval.Mode(*mode) != eval.ModeKey && eval.Mode(*mode) != eval.ModeSubscription:
		return fmt.Errorf("-mode must be %q or %q", eval.ModeKey, eval.ModeSubscription)
	case *parallel < 1:
		return errors.New("-parallel must be at least 1")
	}

	tasks, err := eval.LoadTasks(*tasksDir)
	if err != nil {
		return err
	}
	if ids := splitList(*only); len(ids) > 0 {
		tasks = slices.DeleteFunc(tasks, func(t eval.Task) bool { return !slices.Contains(ids, t.ID) })
	}
	if len(tasks) == 0 {
		return fmt.Errorf("no tasks found in %s", *tasksDir)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg := eval.Config{
		Gateway: *gateway, GatewayKey: *key, AdminToken: *adminToken,
		Mode: eval.Mode(*mode), Claude: *claude, HTTP: &http.Client{Timeout: 30 * time.Second},
	}
	known, err := eval.Routes(ctx, cfg)
	if err != nil {
		return fmt.Errorf("read routes from the gateway: %w", err)
	}
	for _, r := range routeList {
		if !slices.Contains(known, r) {
			return fmt.Errorf("route %q does not exist on the gateway", r)
		}
	}

	total := len(tasks) * len(routeList)
	fmt.Printf("%d tasks x %d routes = %d Claude Code runs (mode %s, %d at a time).\n", len(tasks), len(routeList), total, *mode, *parallel)
	fmt.Println("Each run lets a model edit files and run go or python3 commands in a temporary copy of the task on this machine.")
	fmt.Println("API calls are billed by your providers. Subscription mode uses your Claude plan limits.")
	if !*yes {
		fmt.Println("Run again with -yes to start.")
		return nil
	}

	type job struct {
		task  eval.Task
		route string
	}
	jobs := make(chan job)
	results := make([]eval.Result, 0, total)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range *parallel {
		wg.Go(func() {
			for j := range jobs {
				res := eval.Run(ctx, cfg, j.task, j.route)
				mu.Lock()
				results = append(results, res)
				status := "FAIL"
				if res.Passed {
					status = "PASS"
				}
				fmt.Printf("[%d/%d] %-8s %-28s route=%s time=%s api=$%.4f subscription=$%.4f %s\n",
					len(results), total, status, res.Task, res.Route,
					(time.Duration(res.DurationMS) * time.Millisecond).Round(time.Second), res.CostUSD, res.SubscriptionValueUSD, res.Error)
				mu.Unlock()
			}
		})
	}
dispatch:
	for _, t := range tasks {
		for _, r := range routeList {
			select {
			case jobs <- job{t, r}:
			case <-ctx.Done():
				break dispatch
			}
		}
	}
	close(jobs)
	wg.Wait()

	slices.SortFunc(results, func(a, b eval.Result) int {
		return strings.Compare(a.Route+"\x00"+a.Task, b.Route+"\x00"+b.Task)
	})
	summaries := eval.Summarize(results)
	report, err := json.MarshalIndent(map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"mode":         *mode,
		"summaries":    summaries,
		"results":      results,
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, report, 0o644); err != nil {
		return err
	}
	fmt.Println()
	if err := eval.WriteTable(os.Stdout, summaries); err != nil {
		return err
	}
	fmt.Printf("\nFull results: %s\n", *out)
	return ctx.Err()
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
