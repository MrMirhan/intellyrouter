# sessions

Turns the raw clickstream from our web tracker into visit sessions for the
product analytics dashboards.

```python
from sessions import Event, sessionize, top_landing_pages

visits = sessionize(events)                 # list[Session]
top_landing_pages(visits, n=5)              # [("/pricing", 812), ("/", 640), ...]
```

## Rules

- The tracker delivers at-least-once, so the same event (same user, timestamp
  and page) can arrive more than once. Identical events count once.
- Events can arrive in any order. Timestamps are timezone-aware and may use
  different UTC offsets.
- A user's events, in time order, belong to the same session while the gap
  between two consecutive events is at most `gap` (30 minutes by default).
  A longer gap starts a new session. A gap of exactly 30 minutes stays in the
  session.
- Events with the same timestamp keep their input order.
- `sessionize` returns sessions ordered by start time, then by user id.
- `top_landing_pages` counts the first page of each session and returns
  `(page, count)` pairs, most common first; pages with the same count are
  ordered by page path.

A normal day is 50,000-100,000 events from a few thousand users. The nightly
job has to finish well within its time slot.
