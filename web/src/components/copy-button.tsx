import { useEffect, useState } from "react"
import { CheckIcon, CopyIcon } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"

export function CopyButton({
  value,
  label = "Copy",
  iconOnly = false,
}: {
  value: string
  label?: string
  iconOnly?: boolean
}) {
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setCopied(false), 1500)
    return () => clearTimeout(timer)
  }, [copied])

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
    } catch {
      toast.error("Copy failed. Select the text and copy it by hand.")
    }
  }

  if (iconOnly) {
    return (
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        onClick={copy}
        title={label}
        aria-label={copied ? "Copied" : label}
      >
        {copied ? <CheckIcon /> : <CopyIcon />}
      </Button>
    )
  }

  return (
    <Button type="button" variant="outline" size="sm" onClick={copy}>
      {copied ? <CheckIcon /> : <CopyIcon />}
      {copied ? "Copied" : label}
    </Button>
  )
}
