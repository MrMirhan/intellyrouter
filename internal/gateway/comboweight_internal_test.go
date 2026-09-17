package gateway

import (
	"testing"

	"github.com/MrMirhan/intellyrouter/internal/store"
)

func weighted(weights ...int) target {
	members := make([]comboMember, 0, len(weights))
	for i, w := range weights {
		m := comboMember{weight: w}
		m.model.ID = int64(i + 1)
		members = append(members, m)
	}
	return target{combo: &comboTarget{strategy: store.ComboRoundRobin, members: members}}
}

// A member's share of the turns follows its weight.
func TestRoundRobinFollowsWeights(t *testing.T) {
	b, tgt := newComboBalancer(), weighted(3, 1)
	got := make([]int, 2)
	for range 400 {
		got[b.order(tgt)[0]]++
	}
	if got[0] != 300 || got[1] != 100 {
		t.Fatalf("shares = %v, want [300 100]", got)
	}
}

// Equal weights round-robin, which is what combos did before weights existed.
func TestRoundRobinEqualWeightsRotates(t *testing.T) {
	b, tgt := newComboBalancer(), weighted(1, 1, 1)
	var heads []int
	for range 6 {
		heads = append(heads, b.order(tgt)[0])
	}
	for i, want := range []int{0, 1, 2, 0, 1, 2} {
		if heads[i] != want {
			t.Fatalf("heads = %v, want a plain rotation", heads)
		}
	}
}

// The turns interleave rather than arriving in one burst per member.
func TestRoundRobinSpreadsTheHeavyMember(t *testing.T) {
	b, tgt := newComboBalancer(), weighted(2, 1)
	var heads []int
	for range 6 {
		heads = append(heads, b.order(tgt)[0])
	}
	for i, want := range []int{0, 1, 0, 0, 1, 0} {
		if heads[i] != want {
			t.Fatalf("heads = %v, want the light member every third turn", heads)
		}
	}
}

// Every member stays in the order, so a failure still falls through to the rest.
func TestOrderKeepsEveryMember(t *testing.T) {
	b, tgt := newComboBalancer(), weighted(5, 1, 1)
	order := b.order(tgt)
	if len(order) != 3 {
		t.Fatalf("order = %v, want all three members", order)
	}
	seen := map[int]bool{}
	for _, i := range order {
		seen[i] = true
	}
	if len(seen) != 3 {
		t.Fatalf("order = %v, want each member once", order)
	}
}

// Least-used compares load per unit of weight, so a heavier member is picked
// while it carries proportionally less than a lighter one.
func TestLeastUsedWeighsTheLoad(t *testing.T) {
	b, tgt := newComboBalancer(), weighted(4, 1)
	tgt.combo.strategy = store.ComboLeastUsed
	tgt.model.ID = 99
	// Member 0 serves 2 requests, member 1 serves 1: 2/4 is below 1/1.
	b.start(99, 1)
	b.start(99, 1)
	b.start(99, 2)
	if head := b.order(tgt)[0]; head != 0 {
		t.Fatalf("head = %d, want the member with the lighter load per weight", head)
	}
}
