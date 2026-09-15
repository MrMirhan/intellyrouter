# inventory

Stock reservations for checkout. When a customer starts paying, the items are
reserved for a short time so nobody else can buy the last unit. The payment
callback then commits the reservation, or the customer abandons checkout and
the reservation is cancelled or expires.

```python
from datetime import datetime, timezone
from inventory import ReservationService, StockStore

store = StockStore()
store.set_on_hand("MUG-01", 5)
service = ReservationService(store, clock=lambda: datetime.now(timezone.utc))

r = service.reserve("MUG-01", 2)
service.commit(r.id)
service.stock("MUG-01")  # {"on_hand": 3, "reserved": 0, "available": 3}
```

## Rules

- `available` = on hand − quantity held by **active** reservations.
- A reservation is `active`, `committed`, `released` (cancelled) or `expired`.
- A reservation expires at `expires_at` = creation time + TTL (default 15
  minutes). From that moment on it counts as expired.
- `reserve(sku, qty)`: `qty` must be positive (`ValueError`). Reservations that
  are due to expire are expired first, then the reservation succeeds if
  `qty` ≤ available; otherwise `OutOfStock`.
- `commit(id)`: only an active reservation can be committed; otherwise
  `InvalidState`. If it has expired, it is marked expired, its stock is freed,
  and `ReservationExpired` is raised. Committing reduces on-hand stock.
- `cancel(id)`: releases an active reservation. Cancelling a reservation that
  is already released or expired does nothing. Cancelling a committed
  reservation raises `InvalidState`.
- `stock(sku)` and `expire_due()` expire due reservations. Reservations that
  are no longer active are never released a second time.
- Unknown reservation ids raise `UnknownReservation`.

The clock is injected, so the service never reads the system time itself.
