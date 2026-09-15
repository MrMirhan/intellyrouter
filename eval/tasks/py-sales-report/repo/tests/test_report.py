import io
import unittest

from report import monthly_summary, read_orders


def order(order_id, region, placed_at, amount, status="paid"):
    return {
        "order_id": order_id,
        "region": region,
        "placed_at": placed_at,
        "amount": amount,
        "status": status,
    }


FEBRUARY_TOKYO = order("1000", "APAC", "2026-03-01T05:00:00+09:00", "9.99")
MARCH = [
    order("1001", "EMEA", "2026-03-03T10:00:00+01:00", "2.00"),
    order("1002", "EMEA", "2026-03-15T12:00:00+00:00", "3.35"),
    order("1003", "EMEA", "2026-03-16T12:00:00+00:00", "100.00", status="refunded"),
    order("1004", "APAC", "2026-03-02T08:00:00+09:00", "4.65"),
    order("1005", "APAC", "2026-03-20T08:00:00+09:00", "7.00", status="pending"),
    order("1006", "AMER", "2026-03-10T09:00:00-05:00", "4.65"),
]
APRIL = [
    order("1007", "AMER", "2026-03-31T23:30:00-05:00", "10.00"),
    order("1008", "AMER", "2026-04-02T15:00:00-04:00", "1.43"),
    order("1009", "EMEA", "2026-04-03T09:00:00+02:00", "0.57"),
]


def row(month, region, orders, revenue, average, share):
    return {
        "month": month,
        "region": region,
        "orders": orders,
        "revenue": revenue,
        "average": average,
        "share": share,
    }


class MonthlySummaryTest(unittest.TestCase):
    def test_full_report(self):
        rows = [FEBRUARY_TOKYO, *MARCH, *APRIL]
        self.assertEqual(
            monthly_summary(rows),
            [
                row("2026-02", "APAC", 1, "9.99", "9.99", "100.0"),
                row("2026-03", "EMEA", 2, "5.35", "2.68", "36.5"),
                row("2026-03", "AMER", 1, "4.65", "4.65", "31.7"),
                row("2026-03", "APAC", 1, "4.65", "4.65", "31.7"),
                row("2026-04", "AMER", 2, "11.43", "5.72", "95.3"),
                row("2026-04", "EMEA", 1, "0.57", "0.57", "4.8"),
            ],
        )

    def test_average_rounds_half_up(self):
        [emea] = monthly_summary(MARCH[:2])
        self.assertEqual(emea["revenue"], "5.35")
        self.assertEqual(emea["average"], "2.68")

    def test_share_rounds_half_up(self):
        by_region = {r["region"]: r for r in monthly_summary(APRIL)}
        self.assertEqual(by_region["EMEA"]["share"], "4.8")
        self.assertEqual(by_region["AMER"]["share"], "95.3")

    def test_months_are_utc(self):
        [new_york] = monthly_summary([APRIL[0]])
        self.assertEqual(new_york["month"], "2026-04")
        [tokyo] = monthly_summary([FEBRUARY_TOKYO])
        self.assertEqual(tokyo["month"], "2026-02")

    def test_only_paid_orders_count(self):
        rows = [
            order("1", "EMEA", "2026-05-01T10:00:00+00:00", "10.00"),
            order("2", "EMEA", "2026-05-02T10:00:00+00:00", "99.00", status="pending"),
            order("3", "EMEA", "2026-05-03T10:00:00+00:00", "50.00", status="refunded"),
            order("4", "APAC", "2026-05-03T10:00:00+00:00", "5.00", status="pending"),
        ]
        self.assertEqual(monthly_summary(rows), [row("2026-05", "EMEA", 1, "10.00", "10.00", "100.0")])

    def test_equal_revenue_sorted_by_region(self):
        rows = [
            order("1", "LATAM", "2026-06-01T10:00:00+00:00", "3.00"),
            order("2", "APAC", "2026-06-01T10:00:00+00:00", "3.00"),
            order("3", "EMEA", "2026-06-01T10:00:00+00:00", "1.00"),
            order("4", "AMER", "2026-06-01T10:00:00+00:00", "3.00"),
        ]
        self.assertEqual([r["region"] for r in monthly_summary(rows)], ["AMER", "APAC", "LATAM", "EMEA"])

    def test_timestamp_without_offset_is_rejected(self):
        with self.assertRaises(ValueError):
            monthly_summary([order("1", "EMEA", "2026-03-03T10:00:00", "1.00")])

    def test_many_small_amounts_are_exact(self):
        rows = [order(str(i), "EMEA", "2026-07-01T10:00:00+00:00", "0.10") for i in range(3)]
        rows.append(order("x", "AMER", "2026-07-01T10:00:00+00:00", "0.70"))
        by_region = {r["region"]: r for r in monthly_summary(rows)}
        self.assertEqual(by_region["EMEA"]["revenue"], "0.30")
        self.assertEqual(by_region["EMEA"]["average"], "0.10")
        self.assertEqual(by_region["EMEA"]["share"], "30.0")


class ReadOrdersTest(unittest.TestCase):
    def test_reads_csv_export(self):
        export = io.StringIO(
            "order_id,region,placed_at,amount,status\n"
            "1001,EMEA,2026-03-03T10:00:00+01:00,2.00,paid\n"
            "1002,EMEA,2026-03-15T12:00:00+00:00,3.35,paid\n"
        )
        orders = read_orders(export)
        self.assertEqual(orders, MARCH[:2])
        self.assertEqual(monthly_summary(orders)[0]["average"], "2.68")


if __name__ == "__main__":
    unittest.main()
