package billing

import "fmt"

// LineInput is one line of an order.
type LineInput struct {
	SKU      string
	Quantity int64
	Unit     Cents
	Category string
}

// Line is a calculated invoice line.
type Line struct {
	SKU      string
	Net      Cents
	Discount Cents
	Tax      Cents
}

// Invoice holds the calculated lines and totals.
type Invoice struct {
	Lines    []Line
	Subtotal Cents
	Discount Cents
	Tax      Cents
	Total    Cents
}

// Build calculates an invoice. Use a zero Discount for no discount.
func Build(inputs []LineInput, disc Discount, rates TaxRates) (Invoice, error) {
	nets := make([]Cents, len(inputs))
	for i, in := range inputs {
		if in.Quantity <= 0 {
			return Invoice{}, fmt.Errorf("billing: line %d: quantity must be positive", i+1)
		}
		nets[i] = Cents(in.Quantity) * in.Unit
	}
	discounts := disc.allocate(nets)

	var inv Invoice
	for i, in := range inputs {
		tax, err := rates.lineTax(in.Category, nets[i])
		if err != nil {
			return Invoice{}, err
		}
		line := Line{SKU: in.SKU, Net: nets[i], Discount: discounts[i], Tax: tax}
		inv.Lines = append(inv.Lines, line)
		inv.Subtotal += line.Net
		inv.Discount += line.Discount
		inv.Tax += line.Tax
	}
	inv.Total = inv.Subtotal - inv.Discount + inv.Tax
	return inv, nil
}
