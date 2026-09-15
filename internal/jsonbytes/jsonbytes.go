// Package jsonbytes edits top-level fields of a JSON object without re-encoding
// the rest of the document, so forwarded request bodies stay byte-identical.
package jsonbytes

import (
	"bytes"
	"encoding/json"
	"errors"
)

// Span is the byte range [Start, End) of a value inside a JSON document.
type Span struct{ Start, End int }

var ErrNotObject = errors.New("jsonbytes: body is not a JSON object")

type member struct {
	key string
	// from is where the member starts: its key, or the comma before a later member.
	from  int
	value Span
}

func members(body []byte) ([]member, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, ErrNotObject
	}
	var out []member
	for dec.More() {
		from := int(dec.InputOffset())
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, ErrNotObject
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		end := int(dec.InputOffset())
		out = append(out, member{key: key, from: from, value: Span{Start: end - len(raw), End: end}})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return out, nil
}

// TopLevel returns the value span of every top-level key of a JSON object.
func TopLevel(body []byte) (map[string]Span, error) {
	ms, err := members(body)
	if err != nil {
		return nil, err
	}
	spans := make(map[string]Span, len(ms))
	for _, m := range ms {
		spans[m.key] = m.value
	}
	return spans, nil
}

// SetField replaces the value of a top-level key, or appends the key when it
// is missing. value must be valid JSON.
func SetField(body []byte, key string, value []byte) ([]byte, error) {
	spans, err := TopLevel(body)
	if err != nil {
		return nil, err
	}
	if sp, ok := spans[key]; ok {
		return splice(body, sp.Start, sp.End, value), nil
	}
	k, err := json.Marshal(key)
	if err != nil {
		return nil, err
	}
	var ins []byte
	if len(spans) > 0 {
		ins = append(ins, ',')
	}
	ins = append(ins, k...)
	ins = append(ins, ':')
	ins = append(ins, value...)
	end := bytes.LastIndexByte(body, '}')
	return splice(body, end, end, ins), nil
}

// RemoveField deletes a top-level key and reports whether it was present.
func RemoveField(body []byte, key string) ([]byte, bool, error) {
	ms, err := members(body)
	if err != nil {
		return nil, false, err
	}
	for i, m := range ms {
		if m.key != key {
			continue
		}
		if i == 0 && len(ms) > 1 {
			// ms[1].from is the comma after this member; remove it too.
			return splice(body, m.from, ms[1].from+1, nil), true, nil
		}
		return splice(body, m.from, m.value.End, nil), true, nil
	}
	return body, false, nil
}

func splice(b []byte, start, end int, repl []byte) []byte {
	out := make([]byte, 0, len(b)-(end-start)+len(repl))
	out = append(out, b[:start]...)
	out = append(out, repl...)
	return append(out, b[end:]...)
}
