package store

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func insertTestRequest(t *testing.T, s *Store, r Request) int64 {
	t.Helper()
	if r.Route == "" {
		r.Route, r.Strategy, r.Status = "auto", StrategyDirect, "ok"
	}
	id, err := s.InsertRequest(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func countRows(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func withMessages(envelope string, messages ...string) string {
	return fmt.Sprintf(envelope, "["+strings.Join(messages, ",")+"]")
}

func TestContentRoundTrip(t *testing.T) {
	s := openTest(t)
	cases := []struct {
		name  string
		body  string
		count int
	}{
		{"messages between other fields", `{"model":"auto","system":[{"type":"text","text":"be brief"}],"messages":[{"role":"user","content":"hi"}, {"role":"assistant","content":[{"type":"text","text":"hello"}]}],"stream":true}`, 2},
		{"no messages field", `{"model":"auto","max_tokens":5}`, 0},
		{"empty messages", `{"model":"auto","messages":[]}`, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id := insertTestRequest(t, s, Request{TS: 1})
			if err := s.SaveContent(t.Context(), id, Content{CreatedAt: 1, Input: []byte(c.body)}); err != nil {
				t.Fatal(err)
			}
			got, err := s.RequestContent(t.Context(), id, 0)
			if err != nil {
				t.Fatal(err)
			}
			// Messages are joined without the whitespace between them.
			want := strings.ReplaceAll(c.body, "}, {", "},{")
			if string(got.Body) != want || got.MessageCount != c.count {
				t.Fatalf("content = %s (%d messages), want %s (%d)", got.Body, got.MessageCount, want, c.count)
			}
		})
	}
}

func TestContentDedupeAndTail(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	const envelope = `{"model":"auto","system":"be brief","messages":%s,"stream":true}`
	m1, m2, m3 := `{"role":"user","content":"hi"}`, `{"role":"assistant","content":[{"type":"text","text":"hello"}]}`, `{"role":"user","content":"fix it"}`
	out1, out2 := `{"id":"msg_1"}`, `{"id":"msg_2"}`

	first := insertTestRequest(t, s, Request{TS: 1000, SessionID: "s"})
	if err := s.SaveContent(ctx, first, Content{CreatedAt: 1000, Input: []byte(withMessages(envelope, m1, m2)),
		Legs: []LegContent{{Seq: 0, Output: []byte(out1)}}}); err != nil {
		t.Fatal(err)
	}
	second := insertTestRequest(t, s, Request{TS: 2000, SessionID: "s"})
	legs := []LegContent{{Seq: 0, Input: "Checkpoint: plan", Output: []byte(out2)}, {Seq: 1, Output: []byte(out1)}}
	if err := s.SaveContent(ctx, second, Content{CreatedAt: 2000, Input: []byte(withMessages(envelope, m1, m2, m3)), Legs: legs}); err != nil {
		t.Fatal(err)
	}
	uncaptured := insertTestRequest(t, s, Request{TS: 3000, SessionID: "s"})

	// One envelope, three distinct messages, two distinct outputs.
	if n := countRows(t, s, "content_blobs"); n != 6 {
		t.Errorf("content_blobs = %d, want 6", n)
	}
	if n := countRows(t, s, "request_messages"); n != 5 {
		t.Errorf("request_messages = %d, want 5", n)
	}

	tails := []struct {
		tail int
		want string
	}{
		{0, withMessages(envelope, m1, m2, m3)},
		{1, withMessages(envelope, m3)},
		{10, withMessages(envelope, m1, m2, m3)},
	}
	for _, c := range tails {
		got, err := s.RequestContent(ctx, second, c.tail)
		if err != nil {
			t.Fatal(err)
		}
		if string(got.Body) != c.want || got.MessageCount != 3 || !reflect.DeepEqual(got.Legs, legs) {
			t.Errorf("tail %d: %+v, body %s", c.tail, got, got.Body)
		}
	}

	if _, err := s.RequestContent(ctx, uncaptured, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("uncaptured request: err = %v, want ErrNotFound", err)
	}
	if r, _ := s.GetRequest(ctx, second); !r.Captured {
		t.Error("GetRequest.Captured = false for a captured request")
	}
	items, _, err := s.ListRequests(ctx, RequestFilter{})
	if err != nil || items[0].ID != uncaptured || items[0].Captured || !items[1].Captured {
		t.Errorf("ListRequests captured flags = %+v, %v", items, err)
	}
}

func TestSessionContentDeltas(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	const envA, envB = `{"model":"auto","system":"a","messages":%s}`, `{"model":"auto","system":"b","messages":%s}`
	m := func(n int) string { return fmt.Sprintf(`{"role":"user","content":"%d"}`, n) }
	type step struct {
		agent string
		body  string
	}
	steps := []step{
		{"", withMessages(envA, m(1), m(2))},
		{"", withMessages(envA, m(1), m(2), m(3))},
		{"", ""},
		{"sub", withMessages(envA, m(9))},
		{"", withMessages(envB, m(1), m(2), m(3), m(4))},
		// A compacted history shares only its first message with the previous request.
		{"", withMessages(envA, m(1), m(5))},
	}
	for i, st := range steps {
		ts := int64(1000 * (i + 1))
		id := insertTestRequest(t, s, Request{TS: ts, SessionID: "s", AgentID: st.agent, Legs: []Leg{{Role: "direct", Model: "m", Status: "ok"}}})
		if st.body != "" {
			err := s.SaveContent(ctx, id, Content{CreatedAt: ts, Input: []byte(st.body), Legs: []LegContent{{Seq: 0, Output: []byte(fmt.Sprintf(`{"id":"msg_%d"}`, i))}}})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	insertTestRequest(t, s, Request{TS: 500, SessionID: "other"})

	items, err := s.SessionContent(ctx, "s")
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		captured bool
		envelope string
		added    []string
	}{
		{true, withMessages(envA), []string{m(1), m(2)}},
		{true, "", []string{m(3)}},
		{false, "", nil},
		{true, withMessages(envA), []string{m(9)}},
		{true, withMessages(envB), []string{m(4)}},
		{true, withMessages(envA), []string{m(5)}},
	}
	if len(items) != len(want) {
		t.Fatalf("items = %d, want %d", len(items), len(want))
	}
	for i, w := range want {
		it := items[i]
		var added []string
		if it.MessagesAdded != nil {
			added = []string{}
			for _, b := range it.MessagesAdded {
				added = append(added, string(b))
			}
		}
		if it.Captured != w.captured || string(it.Envelope) != w.envelope || !reflect.DeepEqual(added, w.added) || len(it.Request.Legs) != 1 {
			t.Errorf("item %d = captured %v envelope %s added %v legs %d", i, it.Captured, it.Envelope, added, len(it.Request.Legs))
		}
		if w.captured && (len(it.Legs) != 1 || string(it.Legs[0].Output) != fmt.Sprintf(`{"id":"msg_%d"}`, i)) {
			t.Errorf("item %d legs = %+v", i, it.Legs)
		}
	}
}

func TestPruneContent(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	const envelope = `{"model":"auto","messages":%s}`
	shared := `{"role":"user","content":"shared"}`
	old := insertTestRequest(t, s, Request{TS: 1000})
	if err := s.SaveContent(ctx, old, Content{CreatedAt: 1000, Input: []byte(withMessages(envelope, shared, `{"role":"user","content":"old"}`)),
		Legs: []LegContent{{Seq: 0, Input: "guidance", Output: []byte(`{"id":"old"}`)}}}); err != nil {
		t.Fatal(err)
	}
	recent := insertTestRequest(t, s, Request{TS: 5000})
	recentBody := withMessages(envelope, shared, `{"role":"user","content":"new"}`)
	if err := s.SaveContent(ctx, recent, Content{CreatedAt: 5000, Input: []byte(recentBody),
		Legs: []LegContent{{Seq: 0, Output: []byte(`{"id":"new"}`)}}}); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, "content_blobs"); n != 6 {
		t.Fatalf("content_blobs before prune = %d, want 6", n)
	}

	if err := s.PruneContent(ctx, 2000); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestContent(ctx, old, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old content: err = %v, want ErrNotFound", err)
	}
	got, err := s.RequestContent(ctx, recent, 0)
	if err != nil || string(got.Body) != recentBody || string(got.Legs[0].Output) != `{"id":"new"}` {
		t.Fatalf("recent content = %+v, %v", got, err)
	}
	counts := map[string]int{"content_blobs": 4, "request_messages": 2, "leg_content": 1, "request_content": 1, "requests": 2}
	for table, want := range counts {
		if n := countRows(t, s, table); n != want {
			t.Errorf("%s after prune = %d, want %d", table, n, want)
		}
	}
}
