"""Clickstream sessionization."""

from collections import Counter
from dataclasses import dataclass, field
from datetime import datetime, timedelta
from typing import Iterable

DEFAULT_GAP = timedelta(minutes=30)


@dataclass(frozen=True)
class Event:
    user: str
    timestamp: datetime
    page: str


@dataclass
class Session:
    user: str
    start: datetime
    end: datetime
    pages: list[str] = field(default_factory=list)

    @property
    def duration(self) -> timedelta:
        return self.end - self.start


def sessionize(events: Iterable[Event], gap: timedelta = DEFAULT_GAP) -> list[Session]:
    unique: list[Event] = []
    for event in events:
        if event not in unique:
            unique.append(event)

    sessions: list[Session] = []
    for event in sorted(unique, key=lambda e: e.timestamp):
        current = None
        for session in reversed(sessions):
            if session.user == event.user:
                current = session
                break
        if current is not None and event.timestamp - current.end < gap:
            current.end = event.timestamp
            current.pages.append(event.page)
        else:
            sessions.append(Session(event.user, event.timestamp, event.timestamp, [event.page]))
    return sessions


def top_landing_pages(sessions: Iterable[Session], n: int = 10) -> list[tuple[str, int]]:
    counts = Counter(session.pages[0] for session in sessions)
    return counts.most_common(n)
