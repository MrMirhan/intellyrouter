# billing

Invoice calculation for the web shop. All amounts are integer cents
(`billing.Cents`); rates are basis points (1% = 100 bp).

## Rules

These rules come from finance and must match the numbers in the ERP.

1. **Line net** = quantity × unit price. Quantity must be positive.
2. **Rounding.** Every rate calculation rounds to the nearest cent, and half a
   cent rounds away from zero (0.5 becomes 1).
3. **Discount.** A discount has either a percentage (`PercentBP`) or a fixed
   amount (`Fixed`). It applies only when the invoice subtotal is at least
   `MinSubtotal`.
   - A percentage discount is calculated for each line on the line net,
     rounded per line.
   - A fixed discount is capped at the subtotal and split across lines in
     proportion to their net amounts. Each line first gets its share rounded
     down. The cents that remain go one at a time to the lines with the
     largest remainders; on equal remainders, the earlier line comes first.
     The line discounts always add up to the fixed amount.
4. **Tax** is calculated for each line on the line net minus that line's
   discount, with the rate of the line's tax category, rounded per line.
5. **Invoice totals.** Subtotal, discount and tax are the sums of the line
   values. Total = subtotal − discount + tax.
