import unittest
from datetime import date, datetime, timedelta, timezone

from sla import BusinessCalendar, add_business_time, business_time_between, is_breached

CET = timezone(timedelta(hours=1), "CET")
UTC = timezone.utc
H = timedelta(hours=1)


def cet(month, day, hour, minute=0, second=0):
    return datetime(2026, month, day, hour, minute, second, tzinfo=CET)


class AddBusinessTimeTest(unittest.TestCase):
    # March 2026: the 2nd is a Monday, the 6th a Friday, the 7th and 8th a weekend.
    def setUp(self):
        self.cal = BusinessCalendar(tz=CET)

    def test_calendar_dates_used_by_these_tests(self):
        self.assertEqual(date(2026, 3, 2).weekday(), 0)
        self.assertEqual(date(2026, 3, 6).weekday(), 4)

    def test_same_day(self):
        self.assertEqual(add_business_time(self.cal, cet(3, 2, 10), 4 * H), cet(3, 2, 14))

    def test_rolls_over_weekend(self):
        self.assertEqual(add_business_time(self.cal, cet(3, 6, 15), 4 * H), cet(3, 9, 11))

    def test_opened_outside_business_hours(self):
        self.assertEqual(add_business_time(self.cal, cet(3, 7, 12), H), cet(3, 9, 10))
        self.assertEqual(add_business_time(self.cal, cet(3, 2, 7, 30), timedelta(minutes=30)), cet(3, 2, 9, 30))
        self.assertEqual(add_business_time(self.cal, cet(3, 2, 18), H), cet(3, 3, 10))

    def test_deadline_at_end_of_day(self):
        self.assertEqual(add_business_time(self.cal, cet(3, 2, 13), 4 * H), cet(3, 2, 17))
        self.assertEqual(add_business_time(self.cal, cet(3, 2, 9), 8 * H), cet(3, 2, 17))
        self.assertEqual(add_business_time(self.cal, cet(3, 2, 9), 24 * H), cet(3, 4, 17))

    def test_zero_duration(self):
        self.assertEqual(add_business_time(self.cal, cet(3, 2, 11), timedelta(0)), cet(3, 2, 11))
        self.assertEqual(add_business_time(self.cal, cet(3, 7, 11), timedelta(0)), cet(3, 9, 9))

    def test_holidays_are_skipped(self):
        cal = BusinessCalendar(tz=CET, holidays=frozenset({date(2026, 3, 9)}))
        self.assertEqual(add_business_time(cal, cet(3, 6, 16), 2 * H), cet(3, 10, 10))

    def test_opened_at_in_another_time_zone(self):
        opened = datetime(2026, 3, 6, 15, 30, tzinfo=UTC)  # 16:30 in the office
        self.assertEqual(add_business_time(self.cal, opened, H), cet(3, 9, 9, 30))

        opened = datetime(2026, 3, 2, 10, 30, tzinfo=UTC)  # 11:30 in the office
        deadline = add_business_time(self.cal, opened, H)
        self.assertEqual(deadline, cet(3, 2, 12, 30))
        self.assertEqual(deadline.utcoffset(), H, "deadline must be in the calendar's time zone")
        self.assertEqual((deadline.hour, deadline.minute), (12, 30))

    def test_custom_workdays(self):
        cal = BusinessCalendar(tz=CET, workdays=frozenset({5, 6}))
        self.assertEqual(add_business_time(cal, cet(3, 6, 12), 9 * H), cet(3, 8, 10))

    def test_naive_datetime_is_rejected(self):
        with self.assertRaises(ValueError):
            add_business_time(self.cal, datetime(2026, 3, 2, 10), H)


class BreachTest(unittest.TestCase):
    def setUp(self):
        self.cal = BusinessCalendar(tz=CET)
        self.opened = cet(3, 6, 16)  # deadline Monday 11:00 for 3 hours

    def test_before_deadline(self):
        self.assertFalse(is_breached(self.cal, self.opened, 3 * H, now=cet(3, 9, 10, 59)))

    def test_exactly_at_deadline_is_on_time(self):
        self.assertFalse(is_breached(self.cal, self.opened, 3 * H, now=cet(3, 9, 11)))

    def test_after_deadline(self):
        self.assertTrue(is_breached(self.cal, self.opened, 3 * H, now=cet(3, 9, 11, 0, 1)))


class BusinessTimeBetweenTest(unittest.TestCase):
    def test_across_weekend(self):
        cal = BusinessCalendar(tz=CET)
        self.assertEqual(business_time_between(cal, cet(3, 6, 16), cet(3, 9, 10, 30)), timedelta(hours=2, minutes=30))

    def test_holiday_does_not_count(self):
        cal = BusinessCalendar(tz=CET, holidays=frozenset({date(2026, 3, 9)}))
        self.assertEqual(business_time_between(cal, cet(3, 6, 16), cet(3, 9, 10, 30)), H)

    def test_end_before_start(self):
        cal = BusinessCalendar(tz=CET)
        self.assertEqual(business_time_between(cal, cet(3, 3, 12), cet(3, 2, 12)), timedelta(0))


if __name__ == "__main__":
    unittest.main()
