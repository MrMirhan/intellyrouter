import { useSyncExternalStore } from "react"

const mobileQuery = "(max-width: 767px)"

function subscribe(onChange: () => void) {
  const media = window.matchMedia(mobileQuery)
  media.addEventListener("change", onChange)
  return () => media.removeEventListener("change", onChange)
}

export function useIsMobile() {
  return useSyncExternalStore(subscribe, () => window.matchMedia(mobileQuery).matches)
}
