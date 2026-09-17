import type { DirectorEffort, EscalateSettings, GuidedSettings, Route, RouteAdvisor } from "@/lib/types"

export const defaultEscalateSettings: EscalateSettings = {
  classifier: { enabled: false, model_id: 0, target: "top" },
  failure_streak: { enabled: true, threshold: 3, target: "next" },
}

export const defaultGuidedSettings: GuidedSettings = {
  director: { model_id: 0, effort: "medium", max_calls_per_turn: 6, claude_code: false },
  checkpoints: {
    turn_start: true,
    failed_results: 2,
    steps: 0,
    repeats: 3,
    unsure: true,
    review_on_success: true,
  },
  escalate_after: 2,
  de_escalate_after: 5,
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

// routeModelIds lists the models a route calls: its tiers and a guided director.
export function routeModelIds(route: Route): number[] {
  const ids = route.tiers.map((tier) => tier.model_id)
  if (route.strategy === "guided" && route.settings.director?.model_id) {
    ids.push(route.settings.director.model_id)
  }
  return ids
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
    de_escalate_after: route.settings.de_escalate_after ?? defaultGuidedSettings.de_escalate_after,
    consult: route.settings.consult ?? defaultGuidedSettings.consult,
  }
}

export const defaultAdvisorCalls = 6

// AdvisorForm is the route form state for the advisor. choice is "client", "off", or a model row ID.
export interface AdvisorForm {
  choice: string
  effort: DirectorEffort
  max_calls_per_turn: number
}

export function advisorForm(advisor?: RouteAdvisor): AdvisorForm {
  return {
    choice: advisor?.off ? "off" : advisor?.model_id ? String(advisor.model_id) : "client",
    effort: advisor?.effort ?? "",
    max_calls_per_turn: advisor?.max_calls_per_turn || defaultAdvisorCalls,
  }
}

export function advisorSettings(form: AdvisorForm): { advisor?: RouteAdvisor } {
  if (form.choice === "off") return { advisor: { off: true } }
  if (form.choice === "client") return {}
  return {
    advisor: { model_id: Number(form.choice), effort: form.effort, max_calls_per_turn: form.max_calls_per_turn },
  }
}
