"""Business-hours SLA calculations."""

from dataclasses import dataclass
from datetime import date, datetime, time, timedelta, timezone, tzinfo


@dataclass(frozen=True)
class BusinessCalendar:
    tz: tzinfo = timezone.utc
    opens: time = time(9, 0)
    closes: time = time(17, 0)
    workdays: frozenset[int] = frozenset({0, 1, 2, 3, 4})
    holidays: frozenset[date] = frozenset()

    def is_business_day(self, day: date) -> bool:
        return day.weekday() in self.workdays

    def opening(self, day: date) -> datetime:
        return datetime.combine(day, self.opens, tzinfo=self.tz)

    def closing(self, day: date) -> datetime:
        return datetime.combine(day, self.closes, tzinfo=self.tz)


def add_business_time(cal: BusinessCalendar, opened_at: datetime, duration: timedelta) -> datetime:
    """Return the moment when `duration` of business time has passed after opened_at."""
    current = opened_at
    remaining = duration
    day = current.date()
    while True:
        if cal.is_business_day(day):
            start = max(current, cal.opening(day))
            available = cal.closing(day) - start
            if available > timedelta(0):
                if remaining < available:
                    return start + remaining
                remaining -= available
        day += timedelta(days=1)
        current = cal.opening(day)


def business_time_between(cal: BusinessCalendar, start: datetime, end: datetime) -> timedelta:
    """Business time that passes between start and end."""
    start, end = start.astimezone(cal.tz), end.astimezone(cal.tz)
    total = timedelta(0)
    day = start.date()
    while day <= end.date():
        if cal.is_business_day(day):
            lo = max(start, cal.opening(day))
            hi = min(end, cal.closing(day))
            if hi > lo:
                total += hi - lo
        day += timedelta(days=1)
    return total


def is_breached(cal: BusinessCalendar, opened_at: datetime, duration: timedelta, now: datetime) -> bool:
    return now >= add_business_time(cal, opened_at, duration)
