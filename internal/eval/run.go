package eval

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

type Mode string

const (
	// ModeKey authenticates Claude Code with the gateway key in bare mode.
	ModeKey Mode = "key"
	// ModeSubscription keeps the Claude login and sends the gateway key in x-intelly-key.
	ModeSubscription Mode = "subscription"
)

type Config struct {
	Gateway    string
	GatewayKey string
	AdminToken string
	Mode       Mode
	Claude     string
	HTTP       *http.Client
}

type Result struct {
	Task                 string  `json:"task"`
	Route                string  `json:"route"`
	Passed               bool    `json:"passed"`
	DurationMS           int64   `json:"duration_ms"`
	Requests             int     `json:"requests"`
	EscalatedRequests    int     `json:"escalated_requests"`
	CostUSD              float64 `json:"cost_usd"`
	SubscriptionValueUSD float64 `json:"subscription_value_usd"`
	APITokens            int64   `json:"api_tokens"`
	SubscriptionTokens   int64   `json:"subscription_tokens"`
	ClaudeOutput         string  `json:"claude_output"`
	TestOutput           string  `json:"test_output"`
	Error                string  `json:"error,omitempty"`
}

const testTimeout = 5 * time.Minute

// Run solves one task with Claude Code on one route in a temporary copy of the
// task repository, then grades it with the task's tests and reads the cost
// from the gateway ledger.
func Run(ctx context.Context, cfg Config, task Task, route string) Result {
	res := Result{Task: task.ID, Route: route}
	var errs []error
	defer func() {
		if err := errors.Join(errs...); err != nil {
			res.Error = err.Error()
		}
	}()

	dir, err := os.MkdirTemp("", "intelly-eval-"+task.ID+"-")
	if err != nil {
		errs = append(errs, err)
		return res
	}
	defer os.RemoveAll(dir)
	if err := os.CopyFS(dir, os.DirFS(filepath.Join(task.Dir, "repo"))); err != nil {
		errs = append(errs, fmt.Errorf("copy repo: %w", err))
		return res
	}

	session := newSessionID()
	start := time.Now()
	out, err := runClaude(ctx, cfg, task, route, session, dir)
	res.DurationMS = time.Since(start).Milliseconds()
	res.ClaudeOutput = tail(out, 4000)
	if err != nil {
		errs = append(errs, fmt.Errorf("claude: %w", err))
	}

	if err := restoreProtected(task, dir); err != nil {
		errs = append(errs, err)
		return res
	}
	res.Passed, res.TestOutput = runTests(ctx, task, dir)

	u, err := sessionUsage(ctx, cfg, session)
	if err != nil {
		errs = append(errs, fmt.Errorf("ledger: %w", err))
	}
	res.Requests, res.EscalatedRequests = u.requests, u.escalated
	res.CostUSD, res.SubscriptionValueUSD = u.cost, u.subscriptionValue
	res.APITokens, res.SubscriptionTokens = u.apiTokens, u.subscriptionTokens
	return res
}

func runClaude(ctx context.Context, cfg Config, task Task, route, session, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(task.TimeoutSeconds)*time.Second)
	defer cancel()
	args := []string{
		"-p", task.Prompt,
		"--model", route,
		"--output-format", "json",
		"--session-id", session,
		"--no-session-persistence",
		"--strict-mcp-config",
		"--permission-mode", "dontAsk",
		"--allowedTools", allowedTools(task.Language),
	}
	env := append(cleanEnv(),
		"ANTHROPIC_BASE_URL="+cfg.Gateway,
		// Background and subagent calls must hit the route under test too.
		"ANTHROPIC_DEFAULT_OPUS_MODEL="+route,
		"ANTHROPIC_DEFAULT_SONNET_MODEL="+route,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL="+route,
		"CLAUDE_CODE_SUBAGENT_MODEL="+route,
	)
	if cfg.Mode == ModeSubscription {
		// Bare mode never reads the Claude login, so skip user settings instead.
		args = append(args, "--setting-sources", "project")
		env = append(env, "ANTHROPIC_CUSTOM_HEADERS=x-intelly-key: "+cfg.GatewayKey)
	} else {
		args = append(args, "--bare")
		env = append(env, "ANTHROPIC_API_KEY="+cfg.GatewayKey)
	}
	cmd := exec.CommandContext(ctx, cfg.Claude, args...)
	cmd.Dir, cmd.Env = dir, env
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 15 * time.Second
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func allowedTools(language string) string {
	tools := []string{"Read", "Edit", "Write", "Glob", "Grep", "Bash(ls *)", "Bash(cat *)"}
	switch language {
	case "go":
		tools = append(tools, "Bash(go *)", "Bash(gofmt *)")
	case "python":
		tools = append(tools, "Bash(python3 *)")
	}
	return strings.Join(tools, " ")
}

// cleanEnv drops Anthropic and Claude Code variables from this process, which
// may itself run inside Claude Code, so each run is configured only by Run.
func cleanEnv() []string {
	return slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "ANTHROPIC_") || strings.HasPrefix(kv, "CLAUDE_CODE_") || strings.HasPrefix(kv, "CLAUDECODE=")
	})
}

func runTests(ctx context.Context, task Task, dir string) (bool, string) {
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, task.TestCommand[0], task.TestCommand[1:]...)
	cmd.Dir, cmd.Env = dir, cleanEnv()
	out, err := cmd.CombinedOutput()
	return err == nil, tail(string(out), 4000)
}

type usage struct {
	requests           int
	escalated          int
	cost               float64
	subscriptionValue  float64
	apiTokens          int64
	subscriptionTokens int64
}

func sessionUsage(ctx context.Context, cfg Config, session string) (usage, error) {
	var list struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	if err := adminGet(ctx, cfg, "/api/admin/requests?limit=500&session_id="+url.QueryEscape(session), &list); err != nil {
		return usage{}, err
	}
	var u usage
	for _, item := range list.Items {
		var req struct {
			CostUSD              float64 `json:"cost_usd"`
			SubscriptionValueUSD float64 `json:"subscription_value_usd"`
			Legs                 []struct {
				Role             string `json:"role"`
				Billing          string `json:"billing"`
				InputTokens      int64  `json:"input_tokens"`
				OutputTokens     int64  `json:"output_tokens"`
				CacheReadTokens  int64  `json:"cache_read_tokens"`
				CacheWriteTokens int64  `json:"cache_write_tokens"`
			} `json:"legs"`
		}
		if err := adminGet(ctx, cfg, fmt.Sprintf("/api/admin/requests/%d", item.ID), &req); err != nil {
			return u, err
		}
		u.requests++
		u.cost += req.CostUSD
		u.subscriptionValue += req.SubscriptionValueUSD
		escalated := false
		for _, l := range req.Legs {
			tokens := l.InputTokens + l.OutputTokens + l.CacheReadTokens + l.CacheWriteTokens
			switch {
			case l.Billing == "subscription":
				u.subscriptionTokens += tokens
			case l.Role != "classifier":
				u.apiTokens += tokens
			}
			escalated = escalated || l.Role == "escalation"
		}
		if escalated {
			u.escalated++
		}
	}
	return u, nil
}

// Routes returns the names of the routes the gateway serves.
func Routes(ctx context.Context, cfg Config) ([]string, error) {
	var routes []struct {
		Name string `json:"name"`
	}
	if err := adminGet(ctx, cfg, "/api/admin/routes", &routes); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(routes))
	for _, r := range routes {
		names = append(names, r.Name)
	}
	return names, nil
}

func adminGet(ctx context.Context, cfg Config, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.Gateway, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.AdminToken)
	resp, err := cfg.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s: %s", path, resp.Status, tail(string(body), 300))
	}
	return json.Unmarshal(body, v)
}

func newSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
