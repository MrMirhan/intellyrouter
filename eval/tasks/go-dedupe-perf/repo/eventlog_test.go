package eventlog

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

var base = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

func at(ms int) time.Time { return base.Add(time.Duration(ms) * time.Millisecond) }

func TestMergeOrderingAndDuplicates(t *testing.T) {
	shard0 := []Event{
		{"a", at(10), "s0"},
		{"dup-late", at(30), "s0"},
		{"tie", at(40), "s0"},
		{"same-time-1", at(50), "s0"},
	}
	shard1 := []Event{
		{"dup-late", at(20), "s1"},
		{"b", at(25), "s1"},
		{"tie", at(40), "s1"},
		{"same-time-2", at(50), "s1"},
		{"a", at(60), "s1"},
	}
	got := Merge(shard0, shard1)
	want := []Event{
		{"a", at(10), "s0"},
		{"dup-late", at(20), "s1"},
		{"b", at(25), "s1"},
		{"tie", at(40), "s0"},
		{"same-time-1", at(50), "s0"},
		{"same-time-2", at(50), "s1"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Merge:\n got %v\nwant %v", got, want)
	}
}

func TestMergeKeepsInputOrderWithinStream(t *testing.T) {
	got := Merge([]Event{{"x", at(5), "s0"}, {"y", at(5), "s0"}, {"x", at(5), "s0-retry"}})
	want := []Event{{"x", at(5), "s0"}, {"y", at(5), "s0"}}
	if !slices.Equal(got, want) {
		t.Errorf("Merge = %v, want %v", got, want)
	}
}

func TestMergeEmpty(t *testing.T) {
	if got := Merge(); len(got) != 0 {
		t.Errorf("Merge() = %v", got)
	}
	if got := Merge(nil, []Event{}); len(got) != 0 {
		t.Errorf("Merge(nil, empty) = %v", got)
	}
}

func TestExportCSV(t *testing.T) {
	loc := time.FixedZone("CET", 3600)
	events := []Event{
		{"e1", time.Date(2026, 3, 1, 13, 0, 0, 500, loc), "api"},
		{"e2", at(0), `batch, "nightly"`},
	}
	want := "id,time,source\n" +
		"e1,2026-03-01T12:00:00.0000005Z,api\n" +
		"e2,2026-03-01T00:00:00Z,\"batch, \"\"nightly\"\"\"\n"
	if got := ExportCSV(events); got != want {
		t.Errorf("ExportCSV:\n%s\nwant:\n%s", got, want)
	}
}

// A day of traffic: 150,000 unique events over three shards, with every
// fifth event delivered again by the next shard a little later.
func dayOfEvents() ([][]Event, int) {
	const unique = 150_000
	streams := make([][]Event, 3)
	for i := range unique {
		e := Event{ID: fmt.Sprintf("evt-%06d", i), Time: at(i * 3), Source: fmt.Sprintf("shard-%d", i%3)}
		streams[i%3] = append(streams[i%3], e)
		if i%5 == 0 {
			retry := e
			retry.Time = e.Time.Add(2 * time.Millisecond)
			retry.Source += "-retry"
			streams[(i+1)%3] = append(streams[(i+1)%3], retry)
		}
	}
	for _, s := range streams {
		slices.SortStableFunc(s, func(a, b Event) int { return a.Time.Compare(b.Time) })
	}
	return streams, unique
}

func TestFullDayExportFinishesQuickly(t *testing.T) {
	streams, unique := dayOfEvents()
	const deadline = 15 * time.Second

	type result struct {
		events []Event
		csv    string
	}
	done := make(chan result, 1)
	go func() {
		merged := Merge(streams...)
		done <- result{merged, ExportCSV(merged)}
	}()

	select {
	case <-time.After(deadline):
		t.Fatalf("Merge and ExportCSV on %d events did not finish within %v", unique, deadline)
	case r := <-done:
		if len(r.events) != unique {
			t.Fatalf("merged %d events, want %d", len(r.events), unique)
		}
		for i, e := range r.events {
			if want := fmt.Sprintf("evt-%06d", i); e.ID != want || strings.HasSuffix(e.Source, "-retry") {
				t.Fatalf("event %d = %+v, want the first delivery of %s", i, e, want)
			}
		}
		if lines := strings.Count(r.csv, "\n"); lines != unique+1 {
			t.Fatalf("CSV has %d lines, want %d", lines, unique+1)
		}
	}
}
