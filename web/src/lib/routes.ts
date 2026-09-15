import type { EscalateSettings, GuidedSettings, Route } from "@/lib/types"

export const defaultEscalateSettings: EscalateSettings = {
  classifier: { enabled: false, model_id: 0, target: "top" },
  failure_streak: { enabled: true, threshold: 3, target: "next" },
}

export const defaultGuidedSettings: GuidedSettings = {
  director: { model_id: 0, effort: "medium", max_calls_per_turn: 6 },
  checkpoints: { turn_start: true, failed_results: 2, steps: 15, unsure: true, review_on_success: true },
  escalate_after: 2,
  consult: true,
}

export const labelPattern = /^[a-z0-9._-]+$/

export function defaultLabel(modelId: string): string {
  return modelId
    .slice(modelId.lastIndexOf("/") + 1)
    .toLowerCase()
    .replace(/[^a-z0-9._-]/gu, "-")
    .replace(/^[-._]+|[-._]+$/g, "")
}

export function escalateSettings(route: Route): EscalateSettings {
  return {
    classifier: { ...defaultEscalateSettings.classifier, ...route.settings.classifier },
    failure_streak: { ...defaultEscalateSettings.failure_streak, ...route.settings.failure_streak },
  }
}

export function guidedSettings(route: Route): GuidedSettings {
  return {
    director: { ...defaultGuidedSettings.director, ...route.settings.director },
    checkpoints: { ...defaultGuidedSettings.checkpoints, ...route.settings.checkpoints },
    escalate_after: route.settings.escalate_after ?? defaultGuidedSettings.escalate_after,
    consult: route.settings.consult ?? defaultGuidedSettings.consult,
  }
}
