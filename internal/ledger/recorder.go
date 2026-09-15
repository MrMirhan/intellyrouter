package ledger

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"intellyrouter/internal/store"
)

// Leg is one upstream call made while serving a client request.
type Leg struct {
	Role       string
	Provider   string
	Model      string
	Usage      Usage
	Price      Price
	Latency    time.Duration
	Status     string
	HTTPStatus int
	StopReason string
	Error      string
}

// Entry is one client request and the upstream calls made for it.
type Entry struct {
	Started     time.Time
	SessionID   string
	AgentID     string
	Route       string
	Strategy    string
	AuthMode    string
	ClientModel string
	Stream      bool
	Status      string
	HTTPStatus  int
	Error       string
	Legs        []Leg
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

// Record stores the entry with its cost and the cost the same executor tokens
// would have had on the reference model.
func (r *Recorder) Record(ctx context.Context, e Entry) {
	ref, err := r.referencePrice(ctx)
	if err != nil {
		r.log.Warn("reference price unavailable", "err", err)
	}
	req := store.Request{
		TS:          e.Started.UnixMilli(),
		SessionID:   e.SessionID,
		AgentID:     e.AgentID,
		Route:       e.Route,
		Strategy:    e.Strategy,
		AuthMode:    e.AuthMode,
		ClientModel: e.ClientModel,
		Stream:      e.Stream,
		Status:      e.Status,
		HTTPStatus:  e.HTTPStatus,
		Error:       e.Error,
		LatencyMS:   time.Since(e.Started).Milliseconds(),
	}
	for i, l := range e.Legs {
		cost := l.Price.Cost(l.Usage)
		req.CostUSD += cost
		if l.Role == RoleDirect || l.Role == RoleExecutor {
			req.ReferenceCostUSD += ref.Cost(l.Usage)
		}
		req.Legs = append(req.Legs, store.Leg{
			Seq:              i,
			Role:             l.Role,
			Provider:         l.Provider,
			Model:            l.Model,
			InputTokens:      l.Usage.Input,
			OutputTokens:     l.Usage.Output,
			CacheReadTokens:  l.Usage.CacheRead,
			CacheWriteTokens: l.Usage.CacheWrite,
			CostUSD:          cost,
			LatencyMS:        l.Latency.Milliseconds(),
			Status:           l.Status,
			StopReason:       l.StopReason,
			Note:             l.Error,
		})
	}
	if _, err := r.store.InsertRequest(ctx, req); err != nil {
		r.log.Error("record request", "err", err)
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
