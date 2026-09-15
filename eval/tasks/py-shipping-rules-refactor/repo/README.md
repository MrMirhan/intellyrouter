# shipping

Shipping cost quotes for the checkout page.

```python
from decimal import Decimal
import shipping

shipping.quote({
    "destination": "FR",
    "service": "express",          # "standard" (default) or "express"
    "coupon": None,                # "FREESHIP" or None
    "items": [
        {"sku": "KB-01", "weight_g": 1200, "qty": 2, "price": Decimal("49.00")},
    ],
})
# {"zone": 2, "kg": 3,
#  "lines": [("base", Decimal("12.30")), ("express", Decimal("6.15"))],
#  "total": Decimal("18.45")}
```

## Pricing

1. **Zones.** DE is zone 1; AT, FR and NL are zone 2; US and JP are zone 3.
   Other destinations are rejected.
2. **Base rate** by zone for the first kilogram plus a rate for each further
   started kilogram of total weight: zone 1 4.90 + 0.50, zone 2 9.90 + 1.20,
   zone 3 24.90 + 4.00.
3. **Express** adds 50% of the cost so far (rounded half up to the cent),
   at least 15.00 in zone 3.
4. **Dangerous goods** add 12.00. They cannot be shipped to zone 3.
5. **FREESHIP coupon** makes shipping free in zone 1 with standard service.
6. **Volume discount**: 20% off the remaining cost (rounded half up) when the
   goods subtotal is at least 100.00, outside zone 3, if there is anything
   left to discount.

Invalid orders raise `ValueError`.
