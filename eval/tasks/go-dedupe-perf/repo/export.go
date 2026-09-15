package eventlog

import (
	"strings"
	"time"
)

// ExportCSV renders events as CSV with an id,time,source header.
func ExportCSV(events []Event) string {
	out := "id,time,source\n"
	for _, e := range events {
		out += csvField(e.ID) + "," + e.Time.UTC().Format(time.RFC3339Nano) + "," + csvField(e.Source) + "\n"
	}
	return out
}

func csvField(s string) string {
	if !strings.ContainsAny(s, ",\"\n") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
