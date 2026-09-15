package eval

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

// newTask creates a Go task whose Add function subtracts.
func newTask(t *testing.T, root string) Task {
	t.Helper()
	dir := filepath.Join(root, "go-add")
	writeFile(t, filepath.Join(dir, "task.json"), `{"id":"go-add","language":"go","difficulty":"easy","prompt":"Fix Add.","test_command":["go","test","./..."],"protected":["calc_test.go"],"timeout_seconds":60}`)
	writeFile(t, filepath.Join(dir, "repo", "go.mod"), "module evaltask\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "repo", "calc.go"), "package calc\n\nfunc Add(a, b int) int { return a - b }\n")
	writeFile(t, filepath.Join(dir, "repo", "calc_test.go"), "package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(2, 3) != 5 {\n\t\tt.Fatal(\"Add(2, 3) != 5\")\n\t}\n}\n")
	tasks, err := LoadTasks(root)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("LoadTasks = %+v, %v", tasks, err)
	}
	return tasks[0]
}

// fakeClaude records its arguments and environment, fixes the bug, and also
// guts the protected test so the grader has to restore it.
const fakeClaude = `#!/bin/sh
printf '%s\n' "$@" > "$FAKE_LOG/args"
env | grep -E '^(ANTHROPIC_|CLAUDE_CODE_)' | sort > "$FAKE_LOG/env"
printf 'package calc\n\nfunc Add(a, b int) int { return a + b }\n' > calc.go
printf 'package calc\n' > calc_test.go
echo '{"type":"result","is_error":false}'
`

func fakeAdmin(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer admin-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/api/admin/requests" && len(r.URL.Query().Get("session_id")) == 36:
			_, _ = io.WriteString(w, `{"items":[{"id":7}],"total":1}`)
		case r.URL.Path == "/api/admin/requests/7":
			_, _ = io.WriteString(w, `{"cost_usd":0.01,"subscription_value_usd":0.2,"legs":[
			  {"role":"classifier","billing":"api","input_tokens":40,"output_tokens":5},
			  {"role":"executor","billing":"api","input_tokens":90,"output_tokens":10},
			  {"role":"escalation","billing":"subscription","input_tokens":40,"output_tokens":10}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRunGradesAndReadsLedger(t *testing.T) {
	root := t.TempDir()
	task := newTask(t, root)
	logDir := t.TempDir()
	claude := filepath.Join(t.TempDir(), "claude")
	writeFile(t, claude, fakeClaude)
	t.Setenv("FAKE_LOG", logDir)
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "must-not-leak")

	for _, mode := range []Mode{ModeKey, ModeSubscription} {
		t.Run(string(mode), func(t *testing.T) {
			admin := fakeAdmin(t)
			cfg := Config{
				Gateway: admin.URL, GatewayKey: "ik_test", Mode: mode, Claude: claude,
				Ledger: AdminLedger{Gateway: admin.URL, Token: "admin-token", HTTP: admin.Client()},
			}
			res := Run(context.Background(), cfg, task, "intelly-claude-auto")
			if !res.Passed || res.Error != "" {
				t.Fatalf("result = %+v", res)
			}
			if res.Requests != 1 || res.EscalatedRequests != 1 || res.APITokens != 100 || res.SubscriptionTokens != 50 || res.CostUSD != 0.01 {
				t.Fatalf("usage = %+v", res)
			}

			args := readFile(t, filepath.Join(logDir, "args"))
			env := readFile(t, filepath.Join(logDir, "env"))
			for _, want := range []string{"--model\nintelly-claude-auto\n", "--permission-mode\ndontAsk\n", "--session-id\n", "--no-session-persistence\n"} {
				if !strings.Contains(args, want) {
					t.Errorf("args missing %q:\n%s", want, args)
				}
			}
			for _, want := range []string{"ANTHROPIC_BASE_URL=" + admin.URL, "ANTHROPIC_DEFAULT_HAIKU_MODEL=intelly-claude-auto"} {
				if !strings.Contains(env, want) {
					t.Errorf("env missing %q:\n%s", want, env)
				}
			}
			if strings.Contains(env, "must-not-leak") {
				t.Error("inherited ANTHROPIC_AUTH_TOKEN reached Claude Code")
			}
			if mode == ModeKey {
				if !strings.Contains(args, "--bare\n") || !strings.Contains(env, "ANTHROPIC_API_KEY=ik_test") {
					t.Errorf("key mode args/env:\n%s\n%s", args, env)
				}
			} else {
				if strings.Contains(args, "--bare") || strings.Contains(env, "ANTHROPIC_API_KEY") ||
					!strings.Contains(args, "--setting-sources\nproject\n") || !strings.Contains(env, "ANTHROPIC_CUSTOM_HEADERS=x-intelly-key: ik_test") {
					t.Errorf("subscription mode args/env:\n%s\n%s", args, env)
				}
			}
		})
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLoadTasksRejectsIncompleteTask(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "bad", "task.json"), `{"id":"bad"}`)
	if _, err := LoadTasks(root); err == nil {
		t.Fatal("accepted a task without prompt and test_command")
	}
}

func TestSummarize(t *testing.T) {
	got := Summarize([]Result{
		{Route: "auto", Passed: true, CostUSD: 0.02, SubscriptionValueUSD: 0.1, DurationMS: 1000},
		{Route: "opus", Passed: true, SubscriptionValueUSD: 0.5, DurationMS: 3000},
		{Route: "auto", Passed: false, CostUSD: 0.04, DurationMS: 3000},
	})
	if len(got) != 2 || got[0].Route != "auto" || got[0].Passed != 1 || got[0].Tasks != 2 || got[0].AvgDurationMS != 2000 {
		t.Fatalf("summaries = %+v", got)
	}
	if want := 0.16; got[0].CostPerSolvedUSD < want-1e-9 || got[0].CostPerSolvedUSD > want+1e-9 {
		t.Fatalf("cost per solved = %v, want %v", got[0].CostPerSolvedUSD, want)
	}
}
