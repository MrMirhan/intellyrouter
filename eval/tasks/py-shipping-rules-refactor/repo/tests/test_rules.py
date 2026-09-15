import importlib
import unittest
from decimal import Decimal as D

import shipping

from tests.test_shipping import item, order


class Island:
    name = "island"

    def apply(self, order, quote):
        if order.get("island"):
            quote.add("island", D("7.50"))


class RulesApiTest(unittest.TestCase):
    def setUp(self):
        try:
            self.rules = importlib.import_module("rules")
        except ModuleNotFoundError as exc:
            self.fail(f"rules module is missing: {exc}")

    def test_default_rules(self):
        default = shipping.DEFAULT_RULES
        self.assertEqual(
            [rule.name for rule in default],
            ["base", "express", "dangerous_goods", "coupon", "volume_discount"],
        )
        classes = [self.rules.BaseRate, self.rules.Express, self.rules.DangerousGoods, self.rules.Coupon, self.rules.VolumeDiscount]
        for rule, cls in zip(default, classes, strict=True):
            self.assertIsInstance(rule, cls)

    def test_explicit_default_rules_give_same_quote(self):
        o = order("AT", item(1500, qty=2, price="60.00", dangerous=True), service="express")
        self.assertEqual(shipping.quote(o, rules=shipping.DEFAULT_RULES), shipping.quote(o))

    def test_rule_can_be_left_out(self):
        rules = [rule for rule in shipping.DEFAULT_RULES if rule.name != "coupon"]
        result = shipping.quote(order("DE", item(1000), coupon="FREESHIP"), rules=rules)
        self.assertEqual(result["lines"], [("base", D("4.90"))])
        self.assertEqual(result["total"], D("4.90"))

    def test_custom_rule_runs_in_its_position(self):
        rules = (*shipping.DEFAULT_RULES[:3], Island(), *shipping.DEFAULT_RULES[3:])
        o = order("DE", item(800, price="150.00"))
        o["island"] = True
        result = shipping.quote(o, rules=rules)
        self.assertEqual(
            result,
            {
                "zone": 1,
                "kg": 1,
                "lines": [("base", D("4.90")), ("island", D("7.50")), ("volume_discount", D("-2.48"))],
                "total": D("9.92"),
            },
        )

    def test_rules_work_on_a_quote_directly(self):
        q = shipping.Quote(zone=3, kg=2, subtotal=D("20.00"))
        o = order("US", item(1500), service="express")
        self.rules.BaseRate().apply(o, q)
        self.rules.Express().apply(o, q)
        self.assertEqual(q.lines, [("base", D("28.90")), ("express", D("15.00"))])
        self.assertEqual(q.total, D("43.90"))

    def test_rule_can_reject_order(self):
        q = shipping.Quote(zone=3, kg=1, subtotal=D("10.00"))
        with self.assertRaises(ValueError):
            self.rules.DangerousGoods().apply(order("US", item(100, dangerous=True)), q)

    def test_empty_quote_total(self):
        self.assertEqual(shipping.Quote(zone=1, kg=1, subtotal=D("0")).total, D("0"))


if __name__ == "__main__":
    unittest.main()
