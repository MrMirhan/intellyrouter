import unittest
from decimal import Decimal as D

import shipping


def item(weight_g, qty=1, price="10.00", dangerous=False):
    return {"sku": "SKU", "weight_g": weight_g, "qty": qty, "price": D(price), "dangerous": dangerous}


def order(destination, *items, service="standard", coupon=None):
    return {"destination": destination, "service": service, "coupon": coupon, "items": list(items)}


class QuoteBehaviorTest(unittest.TestCase):
    """Pins the current pricing so refactors cannot change it."""

    def assertQuote(self, result, zone, kg, lines, total):
        self.assertEqual(result["zone"], zone)
        self.assertEqual(result["kg"], kg)
        self.assertEqual(result["lines"], [(name, D(amount)) for name, amount in lines])
        self.assertEqual(result["total"], D(total))

    def test_base_rate_first_kilogram(self):
        self.assertQuote(shipping.quote(order("DE", item(800))), 1, 1, [("base", "4.90")], "4.90")

    def test_base_rate_started_kilograms(self):
        self.assertQuote(shipping.quote(order("DE", item(1250, qty=2))), 1, 3, [("base", "5.90")], "5.90")

    def test_express_zone_2(self):
        self.assertQuote(
            shipping.quote(order("FR", item(1200, qty=2), service="express")),
            2, 3, [("base", "12.30"), ("express", "6.15")], "18.45",
        )

    def test_express_minimum_in_zone_3(self):
        self.assertQuote(
            shipping.quote(order("US", item(500), service="express")),
            3, 1, [("base", "24.90"), ("express", "15.00")], "39.90",
        )

    def test_dangerous_goods(self):
        self.assertQuote(
            shipping.quote(order("DE", item(1000, dangerous=True))),
            1, 1, [("base", "4.90"), ("dangerous_goods", "12.00")], "16.90",
        )

    def test_dangerous_goods_rejected_in_zone_3(self):
        with self.assertRaises(ValueError):
            shipping.quote(order("JP", item(1000, dangerous=True)))

    def test_freeship_coupon(self):
        self.assertQuote(
            shipping.quote(order("DE", item(1000, dangerous=True), coupon="FREESHIP")),
            1, 1, [("base", "4.90"), ("dangerous_goods", "12.00"), ("coupon", "-16.90")], "0.00",
        )

    def test_freeship_coupon_not_valid_for_express(self):
        self.assertQuote(
            shipping.quote(order("DE", item(1000), service="express", coupon="FREESHIP")),
            1, 1, [("base", "4.90"), ("express", "2.45")], "7.35",
        )

    def test_freeship_coupon_only_in_zone_1(self):
        self.assertQuote(
            shipping.quote(order("NL", item(1000), coupon="FREESHIP")), 2, 1, [("base", "9.90")], "9.90"
        )

    def test_volume_discount(self):
        self.assertQuote(
            shipping.quote(order("AT", item(1500, qty=2, price="60.00"), service="express")),
            2, 3, [("base", "12.30"), ("express", "6.15"), ("volume_discount", "-3.69")], "14.76",
        )

    def test_volume_discount_after_all_surcharges(self):
        self.assertQuote(
            shipping.quote(order("DE", item(3000, price="100.00", dangerous=True), service="express")),
            1, 3, [("base", "5.90"), ("express", "2.95"), ("dangerous_goods", "12.00"), ("volume_discount", "-4.17")],
            "16.68",
        )

    def test_no_volume_discount_in_zone_3(self):
        self.assertQuote(shipping.quote(order("JP", item(900, price="500.00"))), 3, 1, [("base", "24.90")], "24.90")

    def test_no_volume_discount_after_freeship(self):
        self.assertQuote(
            shipping.quote(order("DE", item(900, price="150.00"), coupon="FREESHIP")),
            1, 1, [("base", "4.90"), ("coupon", "-4.90")], "0.00",
        )

    def test_invalid_orders(self):
        with self.assertRaises(ValueError):
            shipping.quote(order("BR", item(100)))
        with self.assertRaises(ValueError):
            shipping.quote(order("DE", item(0)))


if __name__ == "__main__":
    unittest.main()
