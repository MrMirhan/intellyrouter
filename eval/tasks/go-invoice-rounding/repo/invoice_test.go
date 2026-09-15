package billing

import (
	"slices"
	"strings"
	"testing"
)

var rates = TaxRates{"standard": 1900, "reduced": 700, "zero": 0}

type want struct {
	lines                          []Line
	subtotal, discount, tax, total Cents
}

func check(t *testing.T, name string, inputs []LineInput, disc Discount, w want) {
	t.Helper()
	inv, err := Build(inputs, disc, rates)
	if err != nil {
		t.Fatalf("%s: Build error: %v", name, err)
	}
	for _, f := range []struct {
		field     string
		got, want Cents
	}{
		{"Subtotal", inv.Subtotal, w.subtotal},
		{"Discount", inv.Discount, w.discount},
		{"Tax", inv.Tax, w.tax},
		{"Total", inv.Total, w.total},
	} {
		if f.got != f.want {
			t.Errorf("%s: %s = %v, want %v", name, f.field, f.got, f.want)
		}
	}
	if !slices.Equal(inv.Lines, w.lines) {
		t.Errorf("%s: lines\n got %+v\nwant %+v", name, inv.Lines, w.lines)
	}
}

func TestNoDiscountRoundsTaxPerLine(t *testing.T) {
	check(t, "no discount", []LineInput{
		{"book", 3, 1999, "reduced"},
		{"pen", 1, 250, "standard"},
	}, Discount{}, want{
		lines: []Line{
			{"book", 5997, 0, 420},
			{"pen", 250, 0, 48},
		},
		subtotal: 6247, discount: 0, tax: 468, total: 6715,
	})
}

func TestPercentDiscountAtMinimumSubtotal(t *testing.T) {
	check(t, "15% at minimum", []LineInput{
		{"chair", 2, 1875, "standard"},
		{"lamp", 1, 1250, "standard"},
	}, Discount{Code: "SPRING15", PercentBP: 1500, MinSubtotal: 5000}, want{
		lines: []Line{
			{"chair", 3750, 563, 606},
			{"lamp", 1250, 188, 202},
		},
		subtotal: 5000, discount: 751, tax: 808, total: 5057,
	})
}

func TestPercentDiscountBelowMinimumSubtotal(t *testing.T) {
	check(t, "below minimum", []LineInput{
		{"desk", 1, 4999, "standard"},
	}, Discount{Code: "SPRING15", PercentBP: 1500, MinSubtotal: 5000}, want{
		lines:    []Line{{"desk", 4999, 0, 950}},
		subtotal: 4999, discount: 0, tax: 950, total: 5949,
	})
}

func TestFixedDiscountSplitsByLargestRemainder(t *testing.T) {
	check(t, "fixed 10.00", []LineInput{
		{"mug", 3, 500, "standard"},
		{"tea", 1, 700, "reduced"},
		{"card", 4, 200, "zero"},
	}, Discount{Code: "TENOFF", Fixed: 1000}, want{
		lines: []Line{
			{"mug", 1500, 500, 190},
			{"tea", 700, 233, 33},
			{"card", 800, 267, 0},
		},
		subtotal: 3000, discount: 1000, tax: 223, total: 2223,
	})
}

func TestFixedDiscountTiesGoToEarlierLines(t *testing.T) {
	check(t, "fixed tie", []LineInput{
		{"a", 1, 1000, "zero"},
		{"b", 1, 1000, "zero"},
		{"c", 1, 1000, "zero"},
	}, Discount{Fixed: 200}, want{
		lines: []Line{
			{"a", 1000, 67, 0},
			{"b", 1000, 67, 0},
			{"c", 1000, 66, 0},
		},
		subtotal: 3000, discount: 200, tax: 0, total: 2800,
	})
}

func TestFixedDiscountCappedAtSubtotal(t *testing.T) {
	check(t, "fixed above subtotal", []LineInput{
		{"sticker", 2, 600, "standard"},
	}, Discount{Fixed: 5000}, want{
		lines:    []Line{{"sticker", 1200, 1200, 0}},
		subtotal: 1200, discount: 1200, tax: 0, total: 0,
	})
}

func TestBuildErrors(t *testing.T) {
	if _, err := Build([]LineInput{{"x", 0, 100, "standard"}}, Discount{}, rates); err == nil {
		t.Error("quantity 0: want error")
	}
	_, err := Build([]LineInput{{"x", 1, 100, "luxury"}}, Discount{}, rates)
	if err == nil || !strings.Contains(err.Error(), "luxury") {
		t.Errorf("unknown category: err = %v", err)
	}
}

func TestCentsString(t *testing.T) {
	for c, want := range map[Cents]string{0: "0.00", 5: "0.05", 1234: "12.34", -50: "-0.50"} {
		if got := c.String(); got != want {
			t.Errorf("Cents(%d).String() = %q, want %q", int64(c), got, want)
		}
	}
}
