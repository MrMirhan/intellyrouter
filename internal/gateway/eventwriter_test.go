package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestKeepAliveSendsPingsOnlyWhenIdle(t *testing.T) {
	rec := httptest.NewRecorder()
	ew := newEventWriter(rec)
	stop := ew.keepAlive(30 * time.Millisecond)
	time.Sleep(120 * time.Millisecond)
	stop()
	pings := strings.Count(rec.Body.String(), "event: ping\n")
	if pings == 0 {
		t.Fatal("no ping written during an idle stream")
	}

	busy := httptest.NewRecorder()
	ew = newEventWriter(busy)
	stop = ew.keepAlive(60 * time.Millisecond)
	for range 12 {
		if err := ew.write("content_block_delta", []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	if strings.Contains(busy.Body.String(), "event: ping") {
		t.Fatal("ping written while events were flowing")
	}
}
