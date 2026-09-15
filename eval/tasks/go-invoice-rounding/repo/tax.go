package billing

import "fmt"

// TaxRates maps a tax category, such as "standard" or "reduced", to its rate
// in basis points.
type TaxRates map[string]int64

func (t TaxRates) lineTax(category string, taxable Cents) (Cents, error) {
	bp, ok := t[category]
	if !ok {
		return 0, fmt.Errorf("billing: unknown tax category %q", category)
	}
	return applyRate(taxable, bp), nil
}
