package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type result struct {
	code   int
	stdout string
	stderr string
}

func runCLI(t *testing.T, stdin string, args ...string) (res result) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("run panicked: %v (args %q)", r, args)
			res = result{code: -1}
		}
	}()
	code := run(args, strings.NewReader(stdin), &stdout, &stderr)
	return result{code, stdout.String(), stderr.String()}
}

func numbered(from, to int) string {
	var b strings.Builder
	for i := from; i <= to; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaultTenLines(t *testing.T) {
	path := writeFile(t, t.TempDir(), "app.log", numbered(1, 12))
	res := runCLI(t, "", path)
	if res.code != 0 || res.stdout != numbered(3, 12) {
		t.Fatalf("exit %d, stdout %q", res.code, res.stdout)
	}
}

func TestLineCountFlagForms(t *testing.T) {
	path := writeFile(t, t.TempDir(), "app.log", numbered(1, 6))
	for _, args := range [][]string{
		{"-n", "3", path},
		{"-n=3", path},
		{"--lines", "3", path},
		{"--lines=3", path},
		{path, "-n", "3"},
	} {
		res := runCLI(t, "", args...)
		if res.code != 0 {
			t.Errorf("%q: exit code %d, want 0 (stderr %q)", args, res.code, res.stderr)
			continue
		}
		if res.stdout != numbered(4, 6) {
			t.Errorf("%q: stdout %q, want %q", args, res.stdout, numbered(4, 6))
		}
	}
}

func TestZeroLines(t *testing.T) {
	path := writeFile(t, t.TempDir(), "app.log", numbered(1, 3))
	res := runCLI(t, "", "-n", "0", path)
	if res.code != 0 || res.stdout != "" {
		t.Fatalf("exit %d, stdout %q", res.code, res.stdout)
	}
}

func TestHeadersAndQuiet(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.log", numbered(1, 2))
	b := writeFile(t, dir, "b.log", numbered(1, 3))

	res := runCLI(t, "", "-n", "1", a, b)
	want := fmt.Sprintf("==> %s <==\nline 2\n\n==> %s <==\nline 3\n", a, b)
	if res.code != 0 || res.stdout != want {
		t.Errorf("exit %d, stdout %q, want %q", res.code, res.stdout, want)
	}

	res = runCLI(t, "", "-q", "-n", "1", a, b)
	if res.code != 0 || res.stdout != "line 2\nline 3\n" {
		t.Errorf("quiet: exit %d, stdout %q", res.code, res.stdout)
	}
}

func TestStdin(t *testing.T) {
	for _, args := range [][]string{{"-n", "2"}, {"-n", "2", "-"}} {
		res := runCLI(t, numbered(1, 5), args...)
		if res.code != 0 || res.stdout != numbered(4, 5) {
			t.Errorf("%q: exit %d, stdout %q", args, res.code, res.stdout)
		}
	}
}

func TestDoubleDashEndsFlags(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "-q", numbered(1, 3))
	writeFile(t, dir, "--lines=1", numbered(1, 4))
	t.Chdir(dir)

	res := runCLI(t, "", "-n", "1", "--", "-q", "--lines=1")
	want := "==> -q <==\nline 3\n\n==> --lines=1 <==\nline 4\n"
	if res.code != 0 || res.stdout != want {
		t.Fatalf("exit %d, stdout %q, want %q (stderr %q)", res.code, res.stdout, want, res.stderr)
	}
}

func TestUsageErrors(t *testing.T) {
	path := writeFile(t, t.TempDir(), "app.log", numbered(1, 3))
	for _, args := range [][]string{
		{path, "-n"},
		{"--lines"},
		{"-n", "abc", path},
		{"-n", "-3", path},
		{"-n=", path},
		{"--lines=ten", path},
		{"-x", path},
	} {
		res := runCLI(t, "", args...)
		if res.code != 2 {
			t.Errorf("%q: exit code %d, want 2", args, res.code)
			continue
		}
		if res.stdout != "" {
			t.Errorf("%q: stdout %q, want nothing", args, res.stdout)
		}
		if !strings.Contains(res.stderr, "usage: tailn") {
			t.Errorf("%q: stderr %q does not contain the usage line", args, res.stderr)
		}
	}
}

func TestUnreadableFileExitsOne(t *testing.T) {
	dir := t.TempDir()
	ok := writeFile(t, dir, "ok.log", numbered(1, 1))
	missing := filepath.Join(dir, "missing.log")

	res := runCLI(t, "", "-q", missing, ok)
	if res.code != 1 {
		t.Errorf("exit code %d, want 1", res.code)
	}
	if res.stdout != "line 1\n" {
		t.Errorf("stdout %q, want the readable file", res.stdout)
	}
	if !strings.Contains(res.stderr, "missing.log") {
		t.Errorf("stderr %q does not name the missing file", res.stderr)
	}
}
