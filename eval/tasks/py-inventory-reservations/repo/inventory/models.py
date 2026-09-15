from dataclasses import dataclass
from datetime import datetime

ACTIVE = "active"
COMMITTED = "committed"
RELEASED = "released"
EXPIRED = "expired"


@dataclass
class Reservation:
    id: str
    sku: str
    qty: int
    expires_at: datetime
    state: str = ACTIVE

    def is_expired(self, now: datetime) -> bool:
        return now > self.expires_at
