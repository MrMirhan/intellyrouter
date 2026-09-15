package sse

import (
	"io"
	"strings"
	"testing"
)

func TestReaderParsesEvents(t *testing.T) {
	in := "event: message_start\r\ndata: {\"a\":1}\r\n\r\n" +
		": keep-alive comment\n\n" +
		"event: ping\ndata: {\"type\": \"ping\"}\n\n" +
		"data: line1\ndata: line2\n\n" +
		"event: last\ndata: x"
	r := NewReader(strings.NewReader(in))
	want := []Event{
		{Name: "message_start", Data: []byte(`{"a":1}`)},
		{Name: "ping", Data: []byte(`{"type": "ping"}`)},
		{Name: "", Data: []byte("line1\nline2")},
		{Name: "last", Data: []byte("x")},
	}
	for i, w := range want {
		got, err := r.Next()
		if err != nil {
			t.Fatalf("event %d: %v", i, err)
		}
		if got.Name != w.Name || string(got.Data) != string(w.Data) {
			t.Fatalf("event %d = %q %q, want %q %q", i, got.Name, got.Data, w.Name, w.Data)
		}
	}
	if _, err := r.Next(); err != io.EOF {
		t.Fatalf("want io.EOF after the last event, got %v", err)
	}
}
