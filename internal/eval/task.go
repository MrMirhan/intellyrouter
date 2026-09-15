// Package eval runs Claude Code on coding tasks through the gateway and grades
// the result with each task's own tests.
package eval

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

type Task struct {
	ID             string   `json:"id"`
	Language       string   `json:"language"`
	Difficulty     string   `json:"difficulty"`
	Prompt         string   `json:"prompt"`
	TestCommand    []string `json:"test_command"`
	Protected      []string `json:"protected"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	// Dir holds task.json, repo/, and solution.patch.
	Dir string `json:"-"`
}

// LoadTasks reads every root/<id>/task.json, sorted by directory name.
func LoadTasks(root string) ([]Task, error) {
	paths, err := filepath.Glob(filepath.Join(root, "*", "task.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	tasks := make([]Task, 0, len(paths))
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var t Task
		if err := json.Unmarshal(b, &t); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if t.ID == "" || t.Prompt == "" || len(t.TestCommand) == 0 {
			return nil, fmt.Errorf("%s: id, prompt, and test_command are required", p)
		}
		if t.TimeoutSeconds <= 0 {
			t.TimeoutSeconds = 900
		}
		t.Dir = filepath.Dir(p)
		tasks = append(tasks, t)
	}
	return tasks, nil
}

// restoreProtected puts the original test files back so a model cannot pass
// by editing or deleting them.
func restoreProtected(task Task, dir string) error {
	for _, rel := range task.Protected {
		if err := copyFile(filepath.Join(task.Dir, "repo", rel), filepath.Join(dir, rel)); err != nil {
			return fmt.Errorf("restore %s: %w", rel, err)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
