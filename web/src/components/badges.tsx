import { ArrowUpRightIcon, CompassIcon, CrownIcon, LoaderCircleIcon } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { providerTypes } from "@/lib/providers"
import type { Billing, EvalRunStatus, LegRole, ProviderType, RequestStatus } from "@/lib/types"

const successClass = "border-success/40 text-success"

const requestStatusLabels: Record<RequestStatus, string> = {
  ok: "OK",
  upstream_error: "Upstream error",
  error: "Error",
  canceled: "Canceled",
}

export function RequestStatusBadge({ status }: { status: RequestStatus }) {
  if (status === "ok") {
    return (
      <Badge variant="outline" className={successClass}>
        OK
      </Badge>
    )
  }
  if (status === "canceled") {
    return <Badge variant="secondary">Canceled</Badge>
  }
  return <Badge variant="destructive">{requestStatusLabels[status] ?? status}</Badge>
}

export function LegStatusBadge({ status }: { status: string }) {
  if (status === "ok") {
    return (
      <Badge variant="outline" className={successClass}>
        OK
      </Badge>
    )
  }
  return <Badge variant={status === "canceled" ? "secondary" : "destructive"}>{status}</Badge>
}

export function StrategyBadge({ strategy }: { strategy: string }) {
  if (strategy === "escalate") {
    return (
      <Badge variant="secondary">
        <ArrowUpRightIcon />
        Escalate
      </Badge>
    )
  }
  if (strategy === "guided") {
    return (
      <Badge variant="secondary">
        <CompassIcon />
        Guided
      </Badge>
    )
  }
  if (strategy === "direct") {
    return <Badge variant="outline">Direct</Badge>
  }
  return <Badge variant="outline">{strategy || "Unknown"}</Badge>
}

const roleLabels: Record<LegRole, string> = {
  direct: "Direct",
  executor: "Executor",
  escalation: "Escalation",
  classifier: "Classifier",
  director: "Director",
  advisor: "Advisor",
}

export function RoleBadge({ role }: { role: LegRole }) {
  const variant =
    role === "escalation" || role === "director" || role === "advisor" ? "default" : role === "classifier" ? "secondary" : "outline"
  return <Badge variant={variant}>{roleLabels[role] ?? role}</Badge>
}

export function BillingBadge({ billing }: { billing: Billing }) {
  if (billing === "subscription") {
    return (
      <Badge variant="secondary">
        <CrownIcon />
        Subscription
      </Badge>
    )
  }
  return <Badge variant="outline">API</Badge>
}

export function SubscriptionBadge() {
  return (
    <Badge variant="secondary">
      <CrownIcon />
      Subscription
    </Badge>
  )
}

export function ProviderTypeBadge({ type }: { type: ProviderType }) {
  return <Badge variant="outline">{providerTypes[type]?.label ?? type}</Badge>
}

export function EvalStatusBadge({ status }: { status: EvalRunStatus }) {
  if (status === "running") {
    return (
      <Badge variant="secondary">
        <LoaderCircleIcon className="animate-spin" />
        Running
      </Badge>
    )
  }
  if (status === "done") {
    return (
      <Badge variant="outline" className={successClass}>
        Done
      </Badge>
    )
  }
  if (status === "canceled") {
    return <Badge variant="secondary">Canceled</Badge>
  }
  return <Badge variant="destructive">{status === "failed" ? "Failed" : status}</Badge>
}

export function EvalResultBadge({ passed }: { passed: boolean }) {
  if (passed) {
    return (
      <Badge variant="outline" className={successClass}>
        PASS
      </Badge>
    )
  }
  return <Badge variant="destructive">FAIL</Badge>
}
