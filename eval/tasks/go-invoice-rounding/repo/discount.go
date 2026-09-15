package billing

// Discount is a promotion applied to a whole invoice. Set either PercentBP or
// Fixed.
type Discount struct {
	Code        string
	PercentBP   int64
	Fixed       Cents
	MinSubtotal Cents
}

// allocate returns the discount for each line, given the line net amounts.
func (d Discount) allocate(nets []Cents) []Cents {
	out := make([]Cents, len(nets))
	var subtotal Cents
	for _, n := range nets {
		subtotal += n
	}
	if subtotal <= 0 || subtotal <= d.MinSubtotal {
		return out
	}

	if d.PercentBP > 0 {
		for i, n := range nets {
			out[i] = applyRate(n, d.PercentBP)
		}
	}
	if d.Fixed > 0 {
		fixed := min(d.Fixed, subtotal)
		for i, n := range nets {
			out[i] = fixed * n / subtotal
		}
	}
	return out
}
