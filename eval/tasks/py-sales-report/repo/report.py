"""Monthly revenue by region for the finance dashboard."""

import csv
from collections import defaultdict
from typing import IO, Iterable


def read_orders(fileobj: IO[str]) -> list[dict[str, str]]:
    return list(csv.DictReader(fileobj))


def monthly_summary(rows: Iterable[dict[str, str]]) -> list[dict]:
    groups: dict[tuple[str, str], dict] = defaultdict(lambda: {"orders": 0, "revenue": 0.0})
    month_totals: dict[str, float] = defaultdict(float)

    for row in rows:
        if row["status"] == "refunded":
            continue
        month = row["placed_at"][:7]
        amount = float(row["amount"])
        group = groups[(month, row["region"])]
        group["orders"] += 1
        group["revenue"] += amount
        month_totals[month] += amount

    summary = []
    for (month, region), group in groups.items():
        revenue = group["revenue"]
        summary.append(
            {
                "month": month,
                "region": region,
                "orders": group["orders"],
                "revenue": f"{round(revenue, 2):.2f}",
                "average": f"{round(revenue / group['orders'], 2):.2f}",
                "share": f"{round(100 * revenue / month_totals[month], 1):.1f}",
            }
        )
    summary.sort(key=lambda r: (r["month"], -float(r["revenue"])))
    return summary
