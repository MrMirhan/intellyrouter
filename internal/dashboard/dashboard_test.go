package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHandler(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":         {Data: []byte("<div id=root></div>")},
		"assets/app-1a2b.js": {Data: []byte("console.log(1)")},
	}
	h := Handler(assets)
	cases := []struct {
		path, body, cache string
	}{
		{"/", "<div id=root></div>", "no-cache"},
		{"/requests/42", "<div id=root></div>", "no-cache"},
		{"/assets/app-1a2b.js", "console.log(1)", "public, max-age=31536000, immutable"},
		{"/assets/missing.js", "<div id=root></div>", "no-cache"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != c.body || rec.Header().Get("Cache-Control") != c.cache {
			t.Errorf("GET %s = %d %q cache=%q", c.path, rec.Code, rec.Body.String(), rec.Header().Get("Cache-Control"))
		}
	}

	rec := httptest.NewRecorder()
	Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "make build") {
		t.Errorf("nil assets = %d %q", rec.Code, rec.Body.String())
	}
}
