package jsonbytes

import (
	"encoding/json"
	"testing"
)

func TestSetFieldReplacesOnlyTheValue(t *testing.T) {
	body := []byte(`{"model": "a",  "system":[{"type":"text","text":"<x> & y"}], "n":1.50}`)
	got, err := SetField(body, "model", []byte(`"claude-opus-5"`))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"model": "claude-opus-5",  "system":[{"type":"text","text":"<x> & y"}], "n":1.50}`
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestSetFieldAppendsMissingKey(t *testing.T) {
	cases := map[string]string{
		`{"a":1}`:           `{"a":1,"model":"m"}`,
		`{}`:                `{"model":"m"}`,
		"{\"a\":[1,2]\n}": "{\"a\":[1,2]\n,\"model\":\"m\"}",
	}
	for in, want := range cases {
		got, err := SetField([]byte(in), "model", []byte(`"m"`))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if string(got) != want {
			t.Errorf("SetField(%s) = %s, want %s", in, got, want)
		}
		if !json.Valid(got) {
			t.Errorf("result is not valid JSON: %s", got)
		}
	}
}

func TestTopLevelSpans(t *testing.T) {
	body := []byte(`{"s":"x\"y", "o":{"k":[1,{"z":null}]},"b":true,"num":-2e3}`)
	spans, err := TopLevel(body)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"s": `"x\"y"`, "o": `{"k":[1,{"z":null}]}`, "b": "true", "num": "-2e3"}
	if len(spans) != len(want) {
		t.Fatalf("got %d spans, want %d", len(spans), len(want))
	}
	for k, v := range want {
		sp := spans[k]
		if got := string(body[sp.Start:sp.End]); got != v {
			t.Errorf("span %q = %s, want %s", k, got, v)
		}
	}
}

func TestTopLevelRejectsNonObject(t *testing.T) {
	if _, err := TopLevel([]byte(`[1]`)); err == nil {
		t.Fatal("want error for a JSON array")
	}
}
