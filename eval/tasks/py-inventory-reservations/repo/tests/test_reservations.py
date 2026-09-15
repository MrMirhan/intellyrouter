import itertools
import unittest
from datetime import datetime, timedelta, timezone

from inventory import (
    COMMITTED,
    EXPIRED,
    RELEASED,
    InvalidState,
    OutOfStock,
    ReservationExpired,
    ReservationService,
    StockStore,
    UnknownReservation,
)


class FakeClock:
    def __init__(self):
        self.now = datetime(2026, 5, 4, 12, 0, tzinfo=timezone.utc)

    def __call__(self):
        return self.now

    def advance(self, **kwargs):
        self.now += timedelta(**kwargs)


class ReservationServiceTest(unittest.TestCase):
    def setUp(self):
        self.clock = FakeClock()
        self.store = StockStore()
        self.store.set_on_hand("MUG", 5)
        self.store.set_on_hand("CAP", 3)
        ids = itertools.count(1)
        self.service = ReservationService(self.store, self.clock, new_id=lambda: f"r{next(ids)}")

    def assertStock(self, sku, on_hand, reserved, available):
        self.assertEqual(
            self.service.stock(sku),
            {"on_hand": on_hand, "reserved": reserved, "available": available},
        )

    def test_reserve_last_unit(self):
        self.service.reserve("CAP", 3)
        self.assertStock("CAP", 3, 3, 0)
        with self.assertRaises(OutOfStock) as ctx:
            self.service.reserve("CAP", 1)
        self.assertEqual(ctx.exception.available, 0)

    def test_reserve_more_than_available(self):
        self.service.reserve("MUG", 4)
        with self.assertRaises(OutOfStock):
            self.service.reserve("MUG", 2)
        self.assertStock("MUG", 5, 4, 1)

    def test_invalid_quantity(self):
        for qty in (0, -1):
            with self.subTest(qty=qty), self.assertRaises(ValueError):
                self.service.reserve("MUG", qty)

    def test_skus_are_independent(self):
        self.service.reserve("MUG", 5)
        self.service.reserve("CAP", 1)
        self.assertStock("MUG", 5, 5, 0)
        self.assertStock("CAP", 3, 1, 2)

    def test_commit_reduces_on_hand(self):
        r = self.service.reserve("MUG", 2)
        self.service.commit(r.id)
        self.assertEqual(r.state, COMMITTED)
        self.assertStock("MUG", 3, 0, 3)

    def test_commit_twice_is_invalid(self):
        r = self.service.reserve("MUG", 2)
        self.service.commit(r.id)
        with self.assertRaises(InvalidState):
            self.service.commit(r.id)
        self.assertStock("MUG", 3, 0, 3)

    def test_expired_reservation_frees_stock_for_new_reservations(self):
        self.service.reserve("CAP", 3)
        self.clock.advance(minutes=15)
        r = self.service.reserve("CAP", 3)
        self.assertEqual(r.expires_at, self.clock.now + timedelta(minutes=15))
        self.assertEqual(self.store.get("r1").state, EXPIRED)
        self.assertStock("CAP", 3, 3, 0)

    def test_reservation_is_active_until_expiry(self):
        self.service.reserve("CAP", 3)
        self.clock.advance(minutes=14, seconds=59)
        with self.assertRaises(OutOfStock):
            self.service.reserve("CAP", 1)

    def test_commit_at_expiry_time_fails(self):
        r = self.service.reserve("MUG", 2)
        self.clock.advance(minutes=15)
        with self.assertRaises(ReservationExpired):
            self.service.commit(r.id)
        self.assertEqual(r.state, EXPIRED)
        self.assertStock("MUG", 5, 0, 5)

    def test_cancel_is_idempotent(self):
        r = self.service.reserve("MUG", 2)
        self.service.cancel(r.id)
        self.service.cancel(r.id)
        self.assertEqual(r.state, RELEASED)
        self.assertStock("MUG", 5, 0, 5)

    def test_cancel_after_expiry_does_nothing(self):
        r = self.service.reserve("MUG", 2)
        self.clock.advance(hours=1)
        self.assertStock("MUG", 5, 0, 5)
        self.service.cancel(r.id)
        self.assertEqual(r.state, EXPIRED)
        self.assertStock("MUG", 5, 0, 5)

    def test_cancel_committed_is_invalid(self):
        r = self.service.reserve("MUG", 2)
        self.service.commit(r.id)
        with self.assertRaises(InvalidState):
            self.service.cancel(r.id)
        self.assertEqual(r.state, COMMITTED)
        self.assertStock("MUG", 3, 0, 3)

    def test_expiry_ignores_finished_reservations(self):
        committed = self.service.reserve("MUG", 2)
        self.service.commit(committed.id)
        cancelled = self.service.reserve("MUG", 1)
        self.service.cancel(cancelled.id)
        self.clock.advance(hours=1)
        self.service.expire_due()
        self.assertEqual((committed.state, cancelled.state), (COMMITTED, RELEASED))
        self.assertStock("MUG", 3, 0, 3)

    def test_unknown_reservation(self):
        with self.assertRaises(UnknownReservation):
            self.service.commit("nope")
        with self.assertRaises(UnknownReservation):
            self.service.cancel("nope")


if __name__ == "__main__":
    unittest.main()
