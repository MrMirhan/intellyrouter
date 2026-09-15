package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"

	"intellyrouter/internal/ledger"
)

type RemoteModel struct {
	ID          string
	DisplayName string
	Context     int64
	Price       ledger.Price
}

// ListModels asks the provider which models it serves. Prices come from the
// provider when it publishes them, otherwise from the built-in Claude table.
func ListModels(ctx context.Context, client *http.Client, c Config) ([]RemoteModel, error) {
	base := c.baseURL()
	switch c.Type {
	case Anthropic, AnthropicCompatible:
		var body struct {
			Data []struct {
				ID             string `json:"id"`
				DisplayName    string `json:"display_name"`
				MaxInputTokens int64  `json:"max_input_tokens"`
			} `json:"data"`
		}
		h := http.Header{}
		h.Set("X-Api-Key", c.APIKey)
		h.Set("Anthropic-Version", anthropicVersion)
		if c.Type == AnthropicCompatible {
			h.Set("Authorization", "Bearer "+c.APIKey)
		}
		if err := getJSON(ctx, client, base+"/v1/models?limit=1000", h, &body); err != nil {
			return nil, err
		}
		out := make([]RemoteModel, 0, len(body.Data))
		for _, m := range body.Data {
			p, _ := ledger.BuiltinPrice(m.ID)
			out = append(out, RemoteModel{ID: m.ID, DisplayName: m.DisplayName, Context: m.MaxInputTokens, Price: p})
		}
		return out, nil

	case OpenRouter:
		var body struct {
			Data []struct {
				ID            string `json:"id"`
				Name          string `json:"name"`
				ContextLength int64  `json:"context_length"`
				Pricing       struct {
					Prompt          string `json:"prompt"`
					Completion      string `json:"completion"`
					InputCacheRead  string `json:"input_cache_read"`
					InputCacheWrite string `json:"input_cache_write"`
				} `json:"pricing"`
			} `json:"data"`
		}
		h := http.Header{}
		h.Set("Authorization", "Bearer "+c.APIKey)
		if err := getJSON(ctx, client, base+"/v1/models", h, &body); err != nil {
			return nil, err
		}
		out := make([]RemoteModel, 0, len(body.Data))
		for _, m := range body.Data {
			out = append(out, RemoteModel{
				ID:          m.ID,
				DisplayName: m.Name,
				Context:     m.ContextLength,
				Price: ledger.Price{
					In:         perMillion(m.Pricing.Prompt),
					Out:        perMillion(m.Pricing.Completion),
					CacheRead:  perMillion(m.Pricing.InputCacheRead),
					CacheWrite: perMillion(m.Pricing.InputCacheWrite),
				},
			})
		}
		return out, nil

	case OpenAI, OpenAICompatible:
		var body struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		h := http.Header{}
		if c.APIKey != "" {
			h.Set("Authorization", "Bearer "+c.APIKey)
		}
		if err := getJSON(ctx, client, base+"/models", h, &body); err != nil {
			return nil, err
		}
		out := make([]RemoteModel, 0, len(body.Data))
		for _, m := range body.Data {
			out = append(out, RemoteModel{ID: m.ID, DisplayName: m.ID})
		}
		return out, nil
	}
	return nil, fmt.Errorf("provider type %q cannot list models", c.Type)
}

// perMillion converts OpenRouter's per-token price string. Negative values
// mark variable pricing and are treated as unknown.
func perMillion(perToken string) float64 {
	v, err := strconv.ParseFloat(perToken, 64)
	if err != nil || v < 0 {
		return 0
	}
	return math.Round(v*1e12) / 1e6
}

func getJSON(ctx context.Context, client *http.Client, url string, h http.Header, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header = h
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		if len(body) > 300 {
			body = body[:300]
		}
		return fmt.Errorf("GET %s: %s: %s", url, resp.Status, body)
	}
	return json.Unmarshal(body, v)
}
