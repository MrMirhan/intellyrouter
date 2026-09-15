package ledger

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"intellyrouter/internal/store"
)

// Leg is one upstream call made while serving a client request.
type Leg struct {
	Role       string
	Provider   string
	Model      string
	Billing    string
	Usage      Usage
	Price      Price
	Latency    time.Duration
	Status     string
	HTTPStatus int
	StopReason string
	// Note explains a routing decision; Error describes a failure.
	Note  string
	Error string
	// Input is text the gateway added to this call, such as director guidance;
	// Output is the message the call produced. Both are set only when content
	// capture is on.
	Input  string
	Output []byte
}

// Entry is one client request and the upstream calls made for it.
type Entry struct {
	Started     time.Time
	SessionID   string
	AgentID     string
	Route       string
	Strategy    string
	ClientModel string
	Stream      bool
	Status      string
	HTTPStatus  int
	Error       string
	Legs        []Leg
	// Reference prices the savings estimate; nil uses the reference model setting.
	Reference *Price
	// Input is the client request body, set only when content capture is on.
	Input []byte
}

func (e *Entry) Finish(status string, httpStatus int, err string) {
	e.Status, e.HTTPStatus, e.Error = status, httpStatus, err
}

type Recorder struct {
	store *store.Store
	log   *slog.Logger
}

func NewRecorder(st *store.Store, log *slog.Logger) *Recorder {
	return &Recorder{store: st, log: log}
}

// Record stores the entry. API legs add to the spend, subscription legs add to
// the subscription value, and API-billed base legs add what the same tokens
// would have cost on the reference model. Captured content is stored after the
// request row.
func (r *Recorder) Record(ctx context.Context, e Entry) {
	var ref Price
	if e.Reference != nil {
		ref = *e.Reference
	} else if p, err := r.referencePrice(ctx); err != nil {
		r.log.Warn("reference price unavailable", "err", err)
	} else {
		ref = p
	}
	req := store.Request{
		TS:          e.Started.UnixMilli(),
		SessionID:   e.SessionID,
		AgentID:     e.AgentID,
		Route:       e.Route,
		Strategy:    e.Strategy,
		ClientModel: e.ClientModel,
		Stream:      e.Stream,
		Status:      e.Status,
		HTTPStatus:  e.HTTPStatus,
		Error:       e.Error,
		LatencyMS:   time.Since(e.Started).Milliseconds(),
	}
	content := store.Content{CreatedAt: req.TS, Input: e.Input}
	for i, l := range e.Legs {
		if l.Billing == "" {
			l.Billing = BillingAPI
		}
		cost := l.Price.Cost(l.Usage)
		if l.Billing == BillingSubscription {
			req.SubscriptionValueUSD += cost
		} else {
			req.CostUSD += cost
			if l.Role == RoleDirect || l.Role == RoleExecutor {
				req.ReferenceCostUSD += ref.Cost(l.Usage)
			}
		}
		req.Legs = append(req.Legs, store.Leg{
			Seq:              i,
			Role:             l.Role,
			Provider:         l.Provider,
			Model:            l.Model,
			Billing:          l.Billing,
			InputTokens:      l.Usage.Input,
			OutputTokens:     l.Usage.Output,
			CacheReadTokens:  l.Usage.CacheRead,
			CacheWriteTokens: l.Usage.CacheWrite,
			CostUSD:          cost,
			LatencyMS:        l.Latency.Milliseconds(),
			Status:           l.Status,
			StopReason:       l.StopReason,
			Note:             strings.Join(slices.DeleteFunc([]string{l.Note, l.Error}, func(s string) bool { return s == "" }), "; "),
		})
		if l.Input != "" || len(l.Output) > 0 {
			content.Legs = append(content.Legs, store.LegContent{Seq: i, Input: l.Input, Output: l.Output})
		}
	}
	id, err := r.store.InsertRequest(ctx, req)
	if err != nil {
		r.log.Error("record request", "err", err)
		return
	}
	if len(content.Input) == 0 && len(content.Legs) == 0 {
		return
	}
	if err := r.store.SaveContent(ctx, id, content); err != nil {
		r.log.Error("record request content", "err", err)
	}
}

func (r *Recorder) referencePrice(ctx context.Context) (Price, error) {
	id, ok, err := r.store.Setting(ctx, ReferenceModelSetting)
	if err != nil {
		return Price{}, err
	}
	if !ok {
		id = DefaultReferenceModel
	}
	if p, ok := BuiltinPrice(id); ok {
		return p, nil
	}
	m, err := r.store.ModelByModelID(ctx, id)
	if err != nil {
		return Price{}, fmt.Errorf("reference model %q: %w", id, err)
	}
	return Price{In: m.PriceIn, Out: m.PriceOut, CacheRead: m.PriceCacheRead, CacheWrite: m.PriceCacheWrite}, nil
}
