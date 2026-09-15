// Package eventlog merges and exports audit events delivered by several shards.
package eventlog

import (
	"slices"
	"sort"
	"time"
)

// Event is one audit event.
type Event struct {
	ID     string
	Time   time.Time
	Source string
}

// Merge combines per-shard streams into one timeline without duplicate IDs.
func Merge(streams ...[]Event) []Event {
	var all []Event
	for _, s := range streams {
		all = append(all, s...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].Time.Before(all[j].Time)
	})

	var out []Event
	for _, e := range all {
		if slices.ContainsFunc(out, func(o Event) bool { return o.ID == e.ID }) {
			continue
		}
		out = append(out, e)
	}
	return out
}
