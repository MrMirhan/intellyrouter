// Package duration parses the human-friendly durations used in config files.
package duration

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type unit struct {
	suffix string
	size   time.Duration
}

var units = []unit{
	{"w", 7 * 24 * time.Hour},
	{"d", 24 * time.Hour},
	{"h", time.Hour},
	{"m", time.Minute},
	{"ms", time.Millisecond},
	{"s", time.Second},
}

// Parse parses a duration such as "90s", "1h30m", "2d" or "1.5h".
func Parse(s string) (time.Duration, error) {
	if s == "" {
		return 0, errors.New("duration: empty string")
	}
	orig := s
	var total time.Duration
	for s != "" {
		i := 0
		for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
			i++
		}
		if i == 0 {
			return 0, fmt.Errorf("duration: missing number in %q", orig)
		}
		num := s[:i]
		s = s[i:]

		u, ok := matchUnit(s)
		if !ok {
			return 0, fmt.Errorf("duration: missing or unknown unit in %q", orig)
		}
		s = s[len(u.suffix):]

		n, err := strconv.Atoi(strings.Split(num, ".")[0])
		if err != nil {
			return 0, fmt.Errorf("duration: invalid number %q in %q", num, orig)
		}
		total += time.Duration(n) * u.size
	}
	return total, nil
}

func matchUnit(s string) (unit, bool) {
	for _, u := range units {
		if strings.HasPrefix(s, u.suffix) {
			return u, true
		}
	}
	return unit{}, false
}

// Duration is a time.Duration that unmarshals from the config syntax.
type Duration time.Duration

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Duration) UnmarshalText(text []byte) error {
	v, err := Parse(string(text))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// String returns the value in time.Duration notation.
func (d Duration) String() string {
	return time.Duration(d).String()
}
