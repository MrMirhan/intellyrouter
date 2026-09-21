package gateway

import (
	"bytes"
	"strings"
	"testing"
)

func TestNameRestorerPutsTheLongNameBack(t *testing.T) {
	names := map[string]string{"mcp__short_ab12cd": longToolName}
	var out bytes.Buffer
	n := newNameRestorer(&out, names)
	if _, err := n.Write([]byte(`event: content_block_start` + "\n" +
		`data: {"type":"content_block_start","content_block":{"type":"tool_use","name":"mcp__short_ab12cd"}}` + "\n\n")); err != nil {
		t.Fatal(err)
	}
	if err := n.Flush(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), longToolName) {
		t.Fatalf("the long name did not come back: %s", out.String())
	}
	if strings.Contains(out.String(), "mcp__short_ab12cd") {
		t.Fatalf("the short name survived: %s", out.String())
	}
}

// A name split across two writes must still be restored.
func TestNameRestorerHandlesASplitName(t *testing.T) {
	short := "mcp__short_ab12cd"
	names := map[string]string{short: longToolName}
	var out bytes.Buffer
	n := newNameRestorer(&out, names)
	whole := `{"name":"` + short + `"}`
	half := len(whole) / 2
	if _, err := n.Write([]byte(whole[:half])); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Write([]byte(whole[half:])); err != nil {
		t.Fatal(err)
	}
	if err := n.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, longToolName) || strings.Contains(got, short) {
		t.Fatalf("split name not restored: %s", got)
	}
}

// Without a map the writer passes bytes through unchanged.
func TestNameRestorerPassesThroughWithoutNames(t *testing.T) {
	var out bytes.Buffer
	n := newNameRestorer(&out, nil)
	body := `{"type":"text","text":"hello"}`
	if _, err := n.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := n.Flush(); err != nil {
		t.Fatal(err)
	}
	if out.String() != body {
		t.Fatalf("passthrough changed the bytes: %s", out.String())
	}
}
