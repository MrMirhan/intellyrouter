// Package sse reads server-sent event streams.
package sse

import (
	"bufio"
	"bytes"
	"io"
)

type Event struct {
	Name string
	Data []byte
}

type Reader struct {
	br *bufio.Reader
}

func NewReader(r io.Reader) *Reader {
	return &Reader{br: bufio.NewReaderSize(r, 64<<10)}
}

// Next returns the next event. Comment lines are skipped. It returns io.EOF
// after the last event.
func (r *Reader) Next() (Event, error) {
	var ev Event
	pending := false
	for {
		line, err := r.br.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			if err == io.EOF && pending {
				return ev, nil
			}
			return Event{}, err
		}
		line = bytes.TrimRight(line, "\r\n")
		if len(line) == 0 {
			if pending {
				return ev, nil
			}
			continue
		}
		if line[0] == ':' {
			continue
		}
		field, value, _ := bytes.Cut(line, []byte(":"))
		value = bytes.TrimPrefix(value, []byte(" "))
		switch string(field) {
		case "event":
			ev.Name = string(value)
			pending = true
		case "data":
			if ev.Data != nil {
				ev.Data = append(ev.Data, '\n')
			}
			ev.Data = append(ev.Data, value...)
			if ev.Data == nil {
				ev.Data = []byte{}
			}
			pending = true
		}
	}
}
