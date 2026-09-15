class ReservationError(Exception):
    pass


class OutOfStock(ReservationError):
    def __init__(self, sku: str, requested: int, available: int):
        super().__init__(f"{sku}: requested {requested}, available {available}")
        self.sku = sku
        self.requested = requested
        self.available = available


class UnknownReservation(ReservationError):
    pass


class InvalidState(ReservationError):
    pass


class ReservationExpired(ReservationError):
    pass
