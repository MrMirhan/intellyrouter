const compact = new Intl.NumberFormat("en-US", { notation: "compact", maximumFractionDigits: 1 })
const integer = new Intl.NumberFormat("en-US")
const relative = new Intl.RelativeTimeFormat("en", { numeric: "auto" })

const units: [Intl.RelativeTimeFormatUnit, number][] = [
  ["year", 31_536_000],
  ["month", 2_592_000],
  ["week", 604_800],
  ["day", 86_400],
  ["hour", 3_600],
  ["minute", 60],
  ["second", 1],
]

const usd = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
})
const subDollarUsd = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  minimumSignificantDigits: 2,
  maximumSignificantDigits: 2,
})

export function formatUsd(value: number): string {
  if (value === 0) {
    return "$0"
  }
  // Two significant digits turn 0.995 and above into "$1.0", so those values take the two-decimal path.
  return Math.abs(value) >= 0.995 ? usd.format(value) : subDollarUsd.format(value)
}

export function formatTokens(value: number): string {
  return compact.format(value)
}

export function formatCount(value: number): string {
  return integer.format(value)
}

export function formatPercent(ratio: number): string {
  return `${(ratio * 100).toFixed(1)}%`
}

export function formatLatency(ms: number): string {
  return ms < 1000 ? `${Math.round(ms)} ms` : `${(ms / 1000).toFixed(1)} s`
}

export function formatDuration(ms: number): string {
  if (ms < 60_000) {
    return formatLatency(ms)
  }
  const seconds = Math.round(ms / 1000)
  if (seconds >= 3600) {
    return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
  }
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
}

export function formatPlural(count: number, word: string): string {
  return `${formatCount(count)} ${word}${count === 1 ? "" : "s"}`
}

export function formatRelative(ms: number, now = Date.now()): string {
  const seconds = (ms - now) / 1000
  const [unit, size] = units.find(([, s]) => Math.abs(seconds) >= s) ?? ["second", 1]
  return relative.format(Math.round(seconds / size), unit)
}

export function formatAbsolute(ms: number): string {
  return new Date(ms).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "medium" })
}

export function formatBucket(ts: number, bucketMs: number): string {
  const day = 86_400_000
  const options: Intl.DateTimeFormatOptions =
    bucketMs >= day
      ? { month: "short", day: "numeric" }
      : bucketMs >= day / 4
        ? { month: "short", day: "numeric", hour: "numeric" }
        : { hour: "numeric", minute: "2-digit" }
  return new Date(ts).toLocaleString(undefined, options)
}
