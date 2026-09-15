"""Shipping cost quotes."""

from decimal import ROUND_HALF_UP, Decimal

CENT = Decimal("0.01")
ZONES = {"DE": 1, "AT": 2, "FR": 2, "NL": 2, "US": 3, "JP": 3}
BASE_RATE = {1: Decimal("4.90"), 2: Decimal("9.90"), 3: Decimal("24.90")}
PER_EXTRA_KG = {1: Decimal("0.50"), 2: Decimal("1.20"), 3: Decimal("4.00")}
EXPRESS_MINIMUM_ZONE_3 = Decimal("15.00")
DANGEROUS_GOODS_FEE = Decimal("12.00")
VOLUME_DISCOUNT_THRESHOLD = Decimal("100.00")


def quote(order: dict) -> dict:
    zone = ZONES.get(order["destination"])
    if zone is None:
        raise ValueError(f"unsupported destination {order['destination']!r}")

    items = order["items"]
    weight_g = sum(item["weight_g"] * item["qty"] for item in items)
    if weight_g <= 0:
        raise ValueError("order has no weight")
    kg = -(-weight_g // 1000)
    subtotal = sum((item["price"] * item["qty"] for item in items), Decimal("0"))
    service = order.get("service", "standard")
    lines = []

    cost = BASE_RATE[zone] + PER_EXTRA_KG[zone] * (kg - 1)
    lines.append(("base", cost))

    if service == "express":
        extra = (cost * Decimal("0.5")).quantize(CENT, rounding=ROUND_HALF_UP)
        if zone == 3:
            extra = max(extra, EXPRESS_MINIMUM_ZONE_3)
        lines.append(("express", extra))
        cost += extra

    if any(item.get("dangerous", False) for item in items):
        if zone == 3:
            raise ValueError("dangerous goods cannot be shipped to zone 3")
        lines.append(("dangerous_goods", DANGEROUS_GOODS_FEE))
        cost += DANGEROUS_GOODS_FEE

    if order.get("coupon") == "FREESHIP" and zone == 1 and service == "standard":
        lines.append(("coupon", -cost))
        cost -= cost

    if cost > 0 and subtotal >= VOLUME_DISCOUNT_THRESHOLD and zone != 3:
        discount = (cost * Decimal("0.2")).quantize(CENT, rounding=ROUND_HALF_UP)
        lines.append(("volume_discount", -discount))
        cost -= discount

    return {"zone": zone, "kg": kg, "lines": lines, "total": cost}
