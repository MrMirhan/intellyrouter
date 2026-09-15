import type { EvalDifficulty, EvalLanguage, EvalMode } from "@/lib/types"

export const evalModes: { value: EvalMode; label: string; option: string }[] = [
  { value: "key", label: "Gateway key", option: "Gateway key (bare mode, API providers)" },
  {
    value: "subscription",
    label: "Claude subscription",
    option: "Claude subscription (uses your Claude Code login)",
  },
]

export const evalLanguageLabels: Record<EvalLanguage, string> = {
  go: "Go",
  python: "Python",
}

export const evalDifficultyLabels: Record<EvalDifficulty, string> = {
  easy: "Easy",
  medium: "Medium",
  hard: "Hard",
}

export function evalModeLabel(mode: string): string {
  return evalModes.find((item) => item.value === mode)?.label ?? mode
}
