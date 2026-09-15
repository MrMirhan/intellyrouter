# eventlog

Builds the audit-log export for the compliance team. Each shard of the
ingestion cluster delivers its own stream of audit events. Delivery is
at-least-once, so the same event can arrive from more than one shard, sometimes
with a later timestamp when a shard retried.

`Merge` turns the shard streams into one timeline:

- events are ordered by time;
- when two events have the same time, the one from the earlier stream comes
  first, and within a stream the input order is kept;
- when an ID appears more than once, only the first event in that order is
  kept (so the earliest delivery wins).

`ExportCSV` writes the timeline as CSV with the header `id,time,source`. Times
are UTC in RFC 3339 format with nanoseconds. Fields that contain a comma, a
quote or a newline are quoted.

A full day of events is about 150,000 rows after deduplication.
