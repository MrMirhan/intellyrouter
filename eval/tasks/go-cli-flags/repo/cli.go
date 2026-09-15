package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const usage = "usage: tailn [-n lines] [-q] [--] [file ...]"

type options struct {
	lines int
	quiet bool
	files []string
}

func parseArgs(args []string) (options, error) {
	opts := options{lines: 10}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-n" || arg == "--lines":
			i++
			n, _ := strconv.Atoi(args[i])
			opts.lines = n
		case arg == "-q" || arg == "--quiet":
			opts.quiet = true
		case strings.HasPrefix(arg, "-") && arg != "-":
			return opts, fmt.Errorf("unknown flag %s", arg)
		default:
			opts.files = append(opts.files, arg)
		}
	}
	return opts, nil
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	opts, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "tailn: %v\n%s\n", err, usage)
		return 2
	}
	if len(opts.files) == 0 {
		opts.files = []string{"-"}
	}

	status := 0
	printed := false
	for _, name := range opts.files {
		lines, err := readLast(name, stdin, opts.lines)
		if err != nil {
			fmt.Fprintf(stderr, "tailn: %v\n", err)
			status = 1
			continue
		}
		if len(opts.files) > 1 && !opts.quiet {
			if printed {
				fmt.Fprintln(stdout)
			}
			fmt.Fprintf(stdout, "==> %s <==\n", name)
		}
		for _, line := range lines {
			fmt.Fprintln(stdout, line)
		}
		printed = true
	}
	return status
}

func readLast(name string, stdin io.Reader, n int) ([]string, error) {
	r := stdin
	if name != "-" {
		f, err := os.Open(name)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	var last []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		last = append(last, sc.Text())
		if len(last) > n {
			last = last[1:]
		}
	}
	return last, sc.Err()
}
