import signal
import unittest
from datetime import datetime, timedelta, timezone

from sessions import Event, Session, sessionize, top_landing_pages

UTC = timezone.utc
BASE = datetime(2026, 6, 1, 10, 0, tzinfo=UTC)


def at(minutes, seconds=0):
    return BASE + timedelta(minutes=minutes, seconds=seconds)


class Deadline:
    """Raises TimeoutError in the main thread when the block runs too long."""

    def __init__(self, seconds):
        self.seconds = seconds

    def __enter__(self):
        self.previous = signal.signal(signal.SIGALRM, self._expired)
        signal.setitimer(signal.ITIMER_REAL, self.seconds)
        return self

    def __exit__(self, *exc):
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, self.previous)
        return False

    def _expired(self, signum, frame):
        raise TimeoutError(f"did not finish within {self.seconds}s")


class SessionizeTest(unittest.TestCase):
    def test_splits_on_gap_longer_than_limit(self):
        events = [
            Event("amy", at(0), "/"),
            Event("amy", at(20), "/pricing"),
            Event("amy", at(50), "/signup"),
            Event("amy", at(81), "/docs"),
        ]
        self.assertEqual(
            sessionize(events),
            [
                Session("amy", at(0), at(50), ["/", "/pricing", "/signup"]),
                Session("amy", at(81), at(81), ["/docs"]),
            ],
        )

    def test_gap_of_exactly_the_limit_stays_in_session(self):
        events = [Event("amy", at(0), "/"), Event("amy", at(30), "/a"), Event("amy", at(60, 1), "/b")]
        sessions = sessionize(events)
        self.assertEqual([s.pages for s in sessions], [["/", "/a"], ["/b"]])
        self.assertEqual(sessions[0].duration, timedelta(minutes=30))

    def test_unsorted_input_and_interleaved_users(self):
        events = [
            Event("bob", at(10), "/b2"),
            Event("amy", at(5), "/a2"),
            Event("bob", at(0), "/b1"),
            Event("amy", at(1), "/a1"),
            Event("bob", at(100), "/b3"),
        ]
        self.assertEqual(
            sessionize(events),
            [
                Session("bob", at(0), at(10), ["/b1", "/b2"]),
                Session("amy", at(1), at(5), ["/a1", "/a2"]),
                Session("bob", at(100), at(100), ["/b3"]),
            ],
        )

    def test_duplicate_events_count_once(self):
        e = Event("amy", at(0), "/")
        self.assertEqual(
            sessionize([e, Event("amy", at(3), "/x"), e, Event("amy", at(3), "/x")]),
            [Session("amy", at(0), at(3), ["/", "/x"])],
        )

    def test_same_timestamp_keeps_input_order(self):
        events = [Event("amy", at(0), "/first"), Event("amy", at(0), "/second")]
        self.assertEqual(sessionize(events)[0].pages, ["/first", "/second"])

    def test_sessions_starting_together_are_ordered_by_user(self):
        events = [Event("zed", at(0), "/"), Event("amy", at(0), "/"), Event("kim", at(0), "/")]
        self.assertEqual([s.user for s in sessionize(events)], ["amy", "kim", "zed"])

    def test_mixed_utc_offsets(self):
        cest = timezone(timedelta(hours=2))
        events = [
            Event("amy", datetime(2026, 6, 1, 12, 25, tzinfo=cest), "/later"),
            Event("amy", at(0), "/earlier"),
            Event("amy", datetime(2026, 6, 1, 12, 0, tzinfo=cest), "/earlier"),
        ]
        self.assertEqual([s.pages for s in sessionize(events)], [["/earlier", "/later"]])

    def test_custom_gap(self):
        events = [Event("amy", at(0), "/"), Event("amy", at(6), "/a")]
        self.assertEqual(len(sessionize(events, gap=timedelta(minutes=5))), 2)

    def test_empty(self):
        self.assertEqual(sessionize([]), [])

    def test_full_day_of_traffic(self):
        events = []
        users = 2000
        for k in range(30):
            for u in range(users):
                ts = BASE + timedelta(seconds=13 * u, minutes=20 * k + 45 * (k // 10))
                event = Event(f"user-{u:04d}", ts, f"/page/{(u + k) % 50}")
                events.append(event)
                if (u + k) % 50 == 0:
                    events.append(event)

        try:
            with Deadline(10):
                sessions = sessionize(events)
        except TimeoutError as exc:
            self.fail(f"sessionize on {len(events)} events {exc}")

        self.assertEqual(len(sessions), users * 3)
        self.assertTrue(all(len(s.pages) == 10 for s in sessions))
        self.assertEqual((sessions[0].user, sessions[0].start), ("user-0000", BASE))
        keys = [(s.start, s.user) for s in sessions]
        self.assertEqual(keys, sorted(keys))


class TopLandingPagesTest(unittest.TestCase):
    def test_counts_first_page_with_ties_by_path(self):
        sessions = [
            Session("a", at(0), at(1), ["/pricing", "/signup"]),
            Session("b", at(0), at(1), ["/docs"]),
            Session("c", at(0), at(1), ["/pricing"]),
            Session("d", at(0), at(1), ["/blog", "/pricing"]),
            Session("e", at(0), at(1), ["/docs"]),
            Session("f", at(0), at(1), ["/about"]),
        ]
        self.assertEqual(
            top_landing_pages(sessions, n=3),
            [("/docs", 2), ("/pricing", 2), ("/about", 1)],
        )


if __name__ == "__main__":
    unittest.main()
