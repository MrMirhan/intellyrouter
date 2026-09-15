import uuid
from datetime import datetime, timedelta
from typing import Callable

from .errors import InvalidState, OutOfStock, ReservationExpired
from .models import ACTIVE, COMMITTED, EXPIRED, RELEASED, Reservation
from .store import StockStore


class ReservationService:
    def __init__(
        self,
        store: StockStore,
        clock: Callable[[], datetime],
        ttl: timedelta = timedelta(minutes=15),
        new_id: Callable[[], str] | None = None,
    ):
        self.store = store
        self.clock = clock
        self.ttl = ttl
        self._new_id = new_id or (lambda: uuid.uuid4().hex)

    def reserve(self, sku: str, qty: int) -> Reservation:
        if qty <= 0:
            raise ValueError("qty must be positive")
        available = self.store.available(sku)
        if qty >= available:
            raise OutOfStock(sku, qty, available)
        reservation = Reservation(self._new_id(), sku, qty, self.clock() + self.ttl)
        self.store.hold(reservation)
        return reservation

    def commit(self, reservation_id: str) -> None:
        reservation = self.store.get(reservation_id)
        if reservation.state != ACTIVE:
            raise InvalidState(f"reservation {reservation_id} is {reservation.state}")
        if reservation.is_expired(self.clock()):
            self._expire(reservation)
            raise ReservationExpired(reservation_id)
        self.store.consume(reservation)
        reservation.state = COMMITTED

    def cancel(self, reservation_id: str) -> None:
        reservation = self.store.get(reservation_id)
        self.store.release(reservation)
        reservation.state = RELEASED

    def expire_due(self) -> None:
        now = self.clock()
        for reservation in self.store.active_reservations():
            if reservation.is_expired(now):
                self._expire(reservation)

    def stock(self, sku: str) -> dict[str, int]:
        self.expire_due()
        return {
            "on_hand": self.store.on_hand(sku),
            "reserved": self.store.reserved(sku),
            "available": self.store.available(sku),
        }

    def _expire(self, reservation: Reservation) -> None:
        self.store.release(reservation)
        reservation.state = EXPIRED
