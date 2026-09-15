# sales-report

Builds the "revenue by region" table on the finance dashboard from the nightly
orders export (`orders.csv`).

```python
from report import read_orders, monthly_summary

with open("orders.csv", newline="") as f:
    rows = monthly_summary(read_orders(f))
```

## Input

One row per order: `order_id,region,placed_at,amount,status`.

- `placed_at` is ISO 8601 and must include a UTC offset, for example
  `2026-03-31T23:30:00-05:00`. A timestamp without an offset raises `ValueError`.
- `amount` is a decimal string in EUR with two decimals.
- `status` is `paid`, `pending` or `refunded`.

## Output

One dict per (month, region) with at least one paid order:

| key       | value |
|-----------|-------|
| `month`   | `YYYY-MM`, the calendar month **in UTC** in which the order was placed |
| `region`  | region code |
| `orders`  | number of paid orders (int) |
| `revenue` | sum of paid amounts, string with 2 decimals |
| `average` | revenue / orders, string with 2 decimals |
| `share`   | percentage of that month's revenue over all regions, string with 1 decimal |

Only `paid` orders count. Money is calculated exactly (no binary floating
point), and every rounded value rounds half up, the same as the finance
spreadsheet: 2.675 becomes 2.68 and 4.75 becomes 4.8.

Rows are sorted by month, then by revenue from high to low, then by region
code.
