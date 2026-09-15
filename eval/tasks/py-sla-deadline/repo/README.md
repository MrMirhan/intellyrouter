# sla

Support-ticket SLA clock. A ticket's first-response SLA is measured in
business time, for example "4 business hours".

```python
from datetime import date, timedelta, timezone
from sla import BusinessCalendar, add_business_time, is_breached

berlin_office = BusinessCalendar(
    tz=timezone(timedelta(hours=1)),
    holidays=frozenset({date(2026, 4, 3)}),
)
deadline = add_business_time(berlin_office, ticket.opened_at, timedelta(hours=4))
if is_breached(berlin_office, ticket.opened_at, timedelta(hours=4), now=clock.now()):
    escalate(ticket)
```

## Rules

- A calendar has a time zone, opening and closing times (default 09:00-17:00),
  working weekdays (default Monday-Friday) and holiday dates. Opening hours
  and dates are in the calendar's time zone, whatever time zone the timestamps
  use.
- Timestamps must be timezone-aware. A naive datetime raises `ValueError`.
- If a ticket is opened outside business time, the clock starts at the next
  opening time.
- Business time only passes on working days that are not holidays, between
  opening and closing.
- A deadline that falls exactly on closing time is that day's closing time,
  not the next morning.
- Deadlines are returned in the calendar's time zone.
- A ticket is breached only when `now` is later than the deadline. A response
  at the exact deadline is on time.

The current time is always passed in (`now=`); the module never reads the
system clock.
