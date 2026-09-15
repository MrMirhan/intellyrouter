import type { EscalateSettings, Route } from "@/lib/types"

export const defaultEscalateSettings: EscalateSettings = {
  classifier: { enabled: false, model_id: 0, target: "top" },
  failure_streak: { enabled: true, threshold: 3, target: "next" },
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
