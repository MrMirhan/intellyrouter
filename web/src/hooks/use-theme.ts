import { useContext } from "react"

import { ThemeContext } from "@/lib/theme"

export function useTheme() {
  const state = useContext(ThemeContext)
  if (!state) {
    throw new Error("useTheme must be used inside ThemeProvider")
  }
  return state
}
