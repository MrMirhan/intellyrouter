// Package claudecli runs the Claude Code CLI for one prompt and returns the
// answer. The gateway uses it to ask a subscription model through the user's
// own Claude Code login, which the gateway never reads.
package claudecli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

type Options struct {
	Binary string
	Model  string
	Effort string
	// AppendSystemPrompt is added after Claude Code's own system prompt.
	AppendSystemPrompt string
	// Input is the prompt, sent on standard input.
	Input string
	// Dir is the working directory. Empty runs in a new temporary directory.
	Dir string
	// SessionID names a saved conversation: Resume continues it, otherwise the
	// run starts it. Empty runs without saving the conversation.
	SessionID string
	Resume    bool
}

// Usage is the token use of the model's answer.
type Usage struct {
	Input        int64
	Output       int64
	CacheRead    int64
	CacheWrite   int64
	CacheWrite1h int64
}

type Result struct {
	Text  string
	Usage Usage
}

// Run starts Claude Code and waits for its answer.
func Run(ctx context.Context, o Options) (Result, error) {
	dir := o.Dir
	if dir == "" {
		tmp, err := os.MkdirTemp("", "intellyrouter-claude-")
		if err != nil {
			return Result{}, err
		}
		defer os.RemoveAll(tmp)
		dir = tmp
	} else if err := os.MkdirAll(dir, 0o700); err != nil {
		return Result{}, err
	}
	cmd := exec.CommandContext(ctx, o.Binary, Args(o)...)
	cmd.Dir = dir
	// Title generation and other background calls would read the prompt again.
	cmd.Env = append(CleanEnv(os.Environ()), "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1")
	cmd.Stdin = strings.NewReader(o.Input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("claude: %w: %s", err, tail(strings.TrimSpace(stderr.String()+" "+stdout.String()), 500))
	}
	return Parse(stdout.Bytes())
}

// Args are the flags for one answer: no tools, and no user or MCP settings, so
// the hooks and plugins of this machine stay out.
func Args(o Options) []string {
	args := []string{
		"-p", "--model", o.Model, "--tools", "", "--append-system-prompt", o.AppendSystemPrompt,
		"--output-format", "json", "--setting-sources", "project", "--strict-mcp-config",
	}
	if o.Effort != "" {
		args = append(args, "--effort", o.Effort)
	}
	switch {
	case o.SessionID == "":
		args = append(args, "--no-session-persistence")
	case o.Resume:
		args = append(args, "--resume", o.SessionID)
	default:
		args = append(args, "--session-id", o.SessionID)
	}
	return args
}

// CleanEnv removes variables that would send Claude Code to a gateway or an
// API key instead of the user's login.
func CleanEnv(env []string) []string {
	return slices.DeleteFunc(slices.Clone(env), func(kv string) bool {
		return strings.HasPrefix(kv, "ANTHROPIC_") || strings.HasPrefix(kv, "CLAUDE_CODE_") || strings.HasPrefix(kv, "CLAUDECODE=")
	})
}

// RemoveSession deletes the files of a saved conversation.
func RemoveSession(id string) error {
	config := os.Getenv("CLAUDE_CONFIG_DIR")
	if config == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		config = filepath.Join(home, ".claude")
	}
	var errs []error
	for _, pattern := range []string{id + ".jsonl", id} {
		paths, err := filepath.Glob(filepath.Join(config, "projects", "*", pattern))
		errs = append(errs, err)
		for _, p := range paths {
			errs = append(errs, os.RemoveAll(p))
		}
	}
	return errors.Join(errs...)
}

// Parse reads the JSON output of claude -p --output-format json.
func Parse(out []byte) (Result, error) {
	var r struct {
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
		Subtype string `json:"subtype"`
		Usage   struct {
			Input         int64 `json:"input_tokens"`
			Output        int64 `json:"output_tokens"`
			CacheRead     int64 `json:"cache_read_input_tokens"`
			CacheWrite    int64 `json:"cache_creation_input_tokens"`
			CacheCreation struct {
				Ephemeral1h int64 `json:"ephemeral_1h_input_tokens"`
			} `json:"cache_creation"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return Result{}, fmt.Errorf("read claude output: %w", err)
	}
	if r.IsError {
		return Result{}, fmt.Errorf("claude returned an error (%s): %s", r.Subtype, tail(r.Result, 500))
	}
	u := r.Usage
	res := Result{
		Text:  strings.TrimSpace(r.Result),
		Usage: Usage{Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite, CacheWrite1h: min(u.CacheCreation.Ephemeral1h, u.CacheWrite)},
	}
	if res.Text == "" {
		return Result{}, errors.New("claude returned no text")
	}
	return res, nil
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
