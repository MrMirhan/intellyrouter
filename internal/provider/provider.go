// Package provider describes upstream LLM providers and builds requests for them.
package provider

import (
	"strings"

	"github.com/MrMirhan/intellyrouter/internal/store"
)

type Type string

const (
	Anthropic           Type = "anthropic"
	AnthropicCompatible Type = "anthropic-compatible"
	// AnthropicSubscription passes the Claude Code user's own login through to
	// Anthropic; the gateway stores no credential for it.
	AnthropicSubscription Type = "anthropic-subscription"
	OpenRouter            Type = "openrouter"
	OpenAI                Type = "openai"
	OpenAICompatible      Type = "openai-compatible"
)

type Format int

const (
	FormatAnthropic Format = iota
	FormatOpenAI
)

func (t Type) Valid() bool {
	switch t {
	case Anthropic, AnthropicCompatible, AnthropicSubscription, OpenRouter, OpenAI, OpenAICompatible:
		return true
	}
	return false
}

func (t Type) Format() Format {
	if t == OpenAI || t == OpenAICompatible {
		return FormatOpenAI
	}
	return FormatAnthropic
}

// NeedsBaseURL reports whether the type has no default endpoint.
func (t Type) NeedsBaseURL() bool {
	return t == AnthropicCompatible || t == OpenAICompatible
}

// NeedsKey reports whether the type cannot work without a stored API key.
func (t Type) NeedsKey() bool {
	return t != OpenAICompatible && t != AnthropicSubscription
}

func (t Type) defaultBaseURL() string {
	switch t {
	case Anthropic, AnthropicSubscription:
		return "https://api.anthropic.com"
	case OpenRouter:
		return "https://openrouter.ai/api"
	case OpenAI:
		return "https://api.openai.com/v1"
	}
	return ""
}

type Config struct {
	Type    Type
	BaseURL string
	APIKey  string
}

func ConfigFor(p store.Provider) Config {
	return Config{Type: Type(p.Type), BaseURL: p.BaseURL, APIKey: p.APIKey}
}

func (c Config) baseURL() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return c.Type.defaultBaseURL()
}
