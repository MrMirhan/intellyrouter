from collections import defaultdict

from .errors import UnknownReservation
from .models import Reservation


class StockStore:
    """In-memory stock levels and reservations."""

    def __init__(self):
        self._on_hand: dict[str, int] = {}
        self._reserved: dict[str, int] = defaultdict(int)
        self._reservations: dict[str, Reservation] = {}

    def set_on_hand(self, sku: str, qty: int) -> None:
        self._on_hand[sku] = qty

    def on_hand(self, sku: str) -> int:
        return self._on_hand.get(sku, 0)

    def reserved(self, sku: str) -> int:
        return self._reserved[sku]

    def available(self, sku: str) -> int:
        return self.on_hand(sku) - self.reserved(sku)

    def get(self, reservation_id: str) -> Reservation:
        try:
            return self._reservations[reservation_id]
        except KeyError:
            raise UnknownReservation(reservation_id) from None

    def active_reservations(self) -> list[Reservation]:
        return list(self._reservations.values())

    def hold(self, reservation: Reservation) -> None:
        self._reservations[reservation.id] = reservation
        self._reserved[reservation.sku] += reservation.qty

    def release(self, reservation: Reservation) -> None:
        self._reserved[reservation.sku] -= reservation.qty

    def consume(self, reservation: Reservation) -> None:
        self._reserved[reservation.sku] -= reservation.qty
        self._on_hand[reservation.sku] -= reservation.qty
