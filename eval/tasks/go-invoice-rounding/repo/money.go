// Package billing computes invoice totals in integer cents.
package billing

import "fmt"

// Cents is an amount of money in the smallest currency unit.
type Cents int64

func (c Cents) String() string {
	sign := ""
	if c < 0 {
		sign = "-"
		c = -c
	}
	return fmt.Sprintf("%s%d.%02d", sign, c/100, c%100)
}

// applyRate returns amount × bp / 10000 in cents.
func applyRate(amount Cents, bp int64) Cents {
	return Cents(int64(amount) * bp / 10000)
}
