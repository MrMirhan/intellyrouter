import type { DirectorEffort } from "@/lib/types"

export const efforts: { value: DirectorEffort; label: string }[] = [
  { value: "", label: "Model default" },
  { value: "low", label: "Low" },
  { value: "medium", label: "Medium" },
  { value: "high", label: "High" },
  { value: "xhigh", label: "Extra high" },
  { value: "max", label: "Max" },
]

// Radix Select reserves the empty string, so the model default uses a placeholder value.
export const defaultEffort = "default"
