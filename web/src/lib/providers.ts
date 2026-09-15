import type { ProviderType } from "@/lib/types"

type Requirement = "required" | "optional" | "none"

export interface ProviderTypeInfo {
  label: string
  description: string
  key: Requirement
  baseUrl: Requirement
  defaultBaseUrl?: string
}

export const providerTypes: Record<ProviderType, ProviderTypeInfo> = {
  anthropic: {
    label: "Anthropic API",
    description: "Anthropic's API, billed per token.",
    key: "required",
    baseUrl: "optional",
    defaultBaseUrl: "https://api.anthropic.com",
  },
  "anthropic-compatible": {
    label: "Anthropic-compatible",
    description: "Any endpoint that accepts the Anthropic Messages API.",
    key: "required",
    baseUrl: "required",
  },
  "anthropic-subscription": {
    label: "Claude subscription",
    description: "Your own Claude login from Claude Code, passed through.",
    key: "none",
    baseUrl: "none",
  },
  openrouter: {
    label: "OpenRouter",
    description: "OpenRouter's model catalog.",
    key: "required",
    baseUrl: "optional",
  },
  openai: {
    label: "OpenAI",
    description: "OpenAI's API.",
    key: "required",
    baseUrl: "optional",
  },
  "openai-compatible": {
    label: "OpenAI-compatible",
    description: "Any endpoint that accepts the OpenAI Chat Completions API.",
    key: "optional",
    baseUrl: "required",
  },
}

export const providerTypeList = Object.keys(providerTypes) as ProviderType[]

export interface ProviderPreset {
  id: string
  label: string
  type: ProviderType
  name: string
  baseUrl: string
  hint?: string
}

export const providerPresets: ProviderPreset[] = [
  {
    id: "deepseek",
    label: "DeepSeek",
    type: "anthropic-compatible",
    name: "DeepSeek",
    baseUrl: "https://api.deepseek.com/anthropic",
    hint: "Models such as deepseek-v4-flash and deepseek-v4-pro.",
  },
  {
    id: "kimi",
    label: "Kimi (Moonshot)",
    type: "anthropic-compatible",
    name: "Kimi",
    baseUrl: "https://api.moonshot.ai/anthropic",
  },
  {
    id: "glm",
    label: "GLM (Z.ai)",
    type: "anthropic-compatible",
    name: "GLM",
    baseUrl: "https://api.z.ai/api/anthropic",
  },
  {
    id: "minimax",
    label: "MiniMax",
    type: "anthropic-compatible",
    name: "MiniMax",
    baseUrl: "https://api.minimax.io/anthropic",
  },
  { id: "openrouter", label: "OpenRouter", type: "openrouter", name: "OpenRouter", baseUrl: "" },
  {
    id: "orcarouter",
    label: "OrcaRouter",
    type: "openai-compatible",
    name: "OrcaRouter",
    baseUrl: "https://api.orcarouter.ai/v1",
  },
  {
    id: "gemini",
    label: "Gemini",
    type: "openai-compatible",
    name: "Gemini",
    baseUrl: "https://generativelanguage.googleapis.com/v1beta/openai",
  },
  {
    id: "ollama",
    label: "Ollama (local)",
    type: "openai-compatible",
    name: "Ollama",
    baseUrl: "http://localhost:11434/v1",
    hint: "Ollama needs no API key.",
  },
  { id: "openai", label: "OpenAI", type: "openai", name: "OpenAI", baseUrl: "" },
  { id: "anthropic", label: "Anthropic API", type: "anthropic", name: "Anthropic", baseUrl: "" },
  {
    id: "subscription",
    label: "Claude subscription",
    type: "anthropic-subscription",
    name: "Claude subscription",
    baseUrl: "",
  },
  { id: "custom", label: "Custom", type: "anthropic-compatible", name: "", baseUrl: "" },
]
