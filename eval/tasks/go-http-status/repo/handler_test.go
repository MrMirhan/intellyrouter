package notes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func do(t *testing.T, h http.Handler, method, target, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func expectStatus(t *testing.T, rec *httptest.ResponseRecorder, method, target string, want int) bool {
	t.Helper()
	if rec.Code != want {
		t.Errorf("%s %s: status %d, want %d (body %q)", method, target, rec.Code, want, rec.Body.String())
		return false
	}
	return true
}

func expectErrorBody(t *testing.T, rec *httptest.ResponseRecorder, what string) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("%s: Content-Type %q, want application/json", what, ct)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error == "" {
		t.Errorf("%s: body %q is not {\"error\": \"...\"}", what, rec.Body.String())
	}
}

func TestCreateNote(t *testing.T) {
	h := NewHandler(NewStore())
	rec := do(t, h, "POST", "/notes", "application/json; charset=utf-8", `{"title":"  Groceries ","body":"milk"}`)
	if !expectStatus(t, rec, "POST", "/notes", http.StatusCreated) {
		return
	}
	if loc := rec.Header().Get("Location"); loc != "/notes/1" {
		t.Errorf("Location %q, want /notes/1", loc)
	}
	var got Note
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	if want := (Note{ID: 1, Title: "Groceries", Body: "milk"}); got != want {
		t.Errorf("created %+v, want %+v", got, want)
	}
}

func TestCreateNoteErrors(t *testing.T) {
	long := `{"title":"` + strings.Repeat("x", 121) + `"}`
	huge := `{"title":"big","body":"` + strings.Repeat("a", 1<<20+100) + `"}`
	tests := []struct {
		name        string
		contentType string
		body        string
		want        int
	}{
		{"malformed json", "application/json", `{"title":`, http.StatusBadRequest},
		{"not an object", "application/json", `["title"]`, http.StatusBadRequest},
		{"missing title", "application/json", `{"body":"x"}`, http.StatusUnprocessableEntity},
		{"blank title", "application/json", `{"title":"   "}`, http.StatusUnprocessableEntity},
		{"title too long", "application/json", long, http.StatusUnprocessableEntity},
		{"wrong content type", "text/plain", `{"title":"x"}`, http.StatusUnsupportedMediaType},
		{"body too large", "application/json", huge, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		h := NewHandler(NewStore())
		rec := do(t, h, "POST", "/notes", tt.contentType, tt.body)
		if expectStatus(t, rec, "POST", "/notes ("+tt.name+")", tt.want) {
			expectErrorBody(t, rec, tt.name)
		}
	}
}

func TestTitleLimitCountsCharacters(t *testing.T) {
	h := NewHandler(NewStore())
	title := strings.Repeat("é", 120)
	rec := do(t, h, "POST", "/notes", "application/json", `{"title":"`+title+`"}`)
	expectStatus(t, rec, "POST", "/notes (120 two-byte characters)", http.StatusCreated)
}

func TestGetNote(t *testing.T) {
	h := NewHandler(NewStore())
	do(t, h, "POST", "/notes", "application/json", `{"title":"first","body":"b"}`)

	rec := do(t, h, "GET", "/notes/1", "", "")
	if expectStatus(t, rec, "GET", "/notes/1", http.StatusOK) {
		var got Note
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got != (Note{1, "first", "b"}) {
			t.Errorf("GET /notes/1 body %q", rec.Body.String())
		}
	}

	for target, want := range map[string]int{
		"/notes/99":  http.StatusNotFound,
		"/notes/abc": http.StatusBadRequest,
		"/notes/0":   http.StatusBadRequest,
		"/notes/-1":  http.StatusBadRequest,
	} {
		rec := do(t, h, "GET", target, "", "")
		if expectStatus(t, rec, "GET", target, want) {
			expectErrorBody(t, rec, "GET "+target)
		}
	}
}

func TestDeleteNote(t *testing.T) {
	h := NewHandler(NewStore())
	do(t, h, "POST", "/notes", "application/json", `{"title":"first"}`)

	rec := do(t, h, "DELETE", "/notes/1", "", "")
	if expectStatus(t, rec, "DELETE", "/notes/1", http.StatusNoContent) && rec.Body.Len() != 0 {
		t.Errorf("DELETE /notes/1: body %q, want empty", rec.Body.String())
	}
	expectStatus(t, do(t, h, "GET", "/notes/1", "", ""), "GET", "/notes/1 after delete", http.StatusNotFound)
	expectStatus(t, do(t, h, "DELETE", "/notes/1", "", ""), "DELETE", "/notes/1 again", http.StatusNotFound)
	expectStatus(t, do(t, h, "DELETE", "/notes/x", "", ""), "DELETE", "/notes/x", http.StatusBadRequest)
}

func TestListNotes(t *testing.T) {
	h := NewHandler(NewStore())
	rec := do(t, h, "GET", "/notes", "", "")
	if expectStatus(t, rec, "GET", "/notes", http.StatusOK) && strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("GET /notes on empty store: body %q, want []", rec.Body.String())
	}

	do(t, h, "POST", "/notes", "application/json", `{"title":"a"}`)
	do(t, h, "POST", "/notes", "application/json", `{"title":"b"}`)
	rec = do(t, h, "GET", "/notes", "", "")
	var got []Note
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got) != 2 || got[0].Title != "a" || got[1].Title != "b" {
		t.Errorf("GET /notes body %q", rec.Body.String())
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := NewHandler(NewStore())
	rec := do(t, h, "PUT", "/notes/1", "application/json", `{"title":"x"}`)
	if expectStatus(t, rec, "PUT", "/notes/1", http.StatusMethodNotAllowed) {
		allow := rec.Header().Get("Allow")
		if !strings.Contains(allow, "GET") || !strings.Contains(allow, "DELETE") {
			t.Errorf("Allow %q, want GET and DELETE", allow)
		}
	}
}
