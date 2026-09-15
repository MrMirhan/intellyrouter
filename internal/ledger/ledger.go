// Package ledger measures token usage and cost of upstream calls and records them.
package ledger

import (
	"maps"
	"regexp"
	"slices"
	"strings"
)

const (
	RoleDirect     = "direct"
	RoleExecutor   = "executor"
	RoleEscalation = "escalation"
	RoleClassifier = "classifier"
	RoleDirector   = "director"
)

const (
	StatusOK            = "ok"
	StatusUpstreamError = "upstream_error"
	StatusError         = "error"
	StatusCanceled      = "canceled"
)

const (
	BillingAPI          = "api"
	BillingSubscription = "subscription"
)

const (
	ReferenceModelSetting = "reference_model"
	DefaultReferenceModel = "claude-fable-5-1"
)

type Usage struct {
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
}

// Price is in USD per million tokens.
type Price struct {
	In         float64
	Out        float64
	CacheRead  float64
	CacheWrite float64
}

func (p Price) Cost(u Usage) float64 {
	return (float64(u.Input)*p.In + float64(u.Output)*p.Out +
		float64(u.CacheRead)*p.CacheRead + float64(u.CacheWrite)*p.CacheWrite) / 1e6
}

// Anthropic first-party prices; CacheWrite is the 5-minute rate.
var builtinPrices = map[string]Price{
	"claude-fable-5-1":  {In: 10, Out: 50, CacheRead: 0.25, CacheWrite: 12.5},
	"claude-fable-5":    {In: 10, Out: 50, CacheRead: 1, CacheWrite: 12.5},
	"claude-opus-5":     {In: 5, Out: 25, CacheRead: 0.5, CacheWrite: 6.25},
	"claude-opus-4-8":   {In: 5, Out: 25, CacheRead: 0.5, CacheWrite: 6.25},
	"claude-opus-4-7":   {In: 5, Out: 25, CacheRead: 0.5, CacheWrite: 6.25},
	"claude-opus-4-6":   {In: 5, Out: 25, CacheRead: 0.5, CacheWrite: 6.25},
	"claude-opus-4-5":   {In: 5, Out: 25, CacheRead: 0.5, CacheWrite: 6.25},
	"claude-opus-4-1":   {In: 15, Out: 75, CacheRead: 1.5, CacheWrite: 18.75},
	"claude-opus-4":     {In: 15, Out: 75, CacheRead: 1.5, CacheWrite: 18.75},
	"claude-sonnet-5":   {In: 2, Out: 10, CacheRead: 0.2, CacheWrite: 2.5},
	"claude-sonnet-4-6": {In: 3, Out: 15, CacheRead: 0.3, CacheWrite: 3.75},
	"claude-sonnet-4-5": {In: 3, Out: 15, CacheRead: 0.3, CacheWrite: 3.75},
	"claude-sonnet-4":   {In: 3, Out: 15, CacheRead: 0.3, CacheWrite: 3.75},
	"claude-haiku-4-5":  {In: 1, Out: 5, CacheRead: 0.1, CacheWrite: 1.25},
}

// BuiltinModelIDs lists the Claude models with known prices, sorted.
func BuiltinModelIDs() []string {
	return slices.Sorted(maps.Keys(builtinPrices))
}

var dateSuffix = regexp.MustCompile(`-\d{8}$`)

// BuiltinPrice looks up a Claude model under its Anthropic or OpenRouter
// spelling, e.g. "claude-haiku-4-5-20251001", "anthropic/claude-haiku-4.5",
// or "claude-opus-5[1m]".
func BuiltinPrice(modelID string) (Price, bool) {
	id := modelID
	if i := strings.IndexByte(id, '['); i >= 0 {
		id = id[:i]
	}
	id = id[strings.LastIndexByte(id, '/')+1:]
	id = strings.ReplaceAll(id, ".", "-")
	id = dateSuffix.ReplaceAllString(id, "")
	p, ok := builtinPrices[id]
	return p, ok
}
