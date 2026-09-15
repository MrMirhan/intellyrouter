import { useEffect, useMemo, useState, type ReactNode } from "react"

import { ThemeContext, type Theme } from "@/lib/theme"

const storageKey = "intellyrouter-theme"
const darkQuery = "(prefers-color-scheme: dark)"

function storedTheme(): Theme {
  const value = localStorage.getItem(storageKey)
  return value === "light" || value === "dark" ? value : "system"
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<Theme>(storedTheme)

  useEffect(() => {
    const media = window.matchMedia(darkQuery)
    const apply = () => {
      const dark = theme === "dark" || (theme === "system" && media.matches)
      document.documentElement.classList.toggle("dark", dark)
      document.documentElement.style.colorScheme = dark ? "dark" : "light"
    }
    apply()
    media.addEventListener("change", apply)
    return () => media.removeEventListener("change", apply)
  }, [theme])

  const value = useMemo(
    () => ({
      theme,
      setTheme: (next: Theme) => {
        localStorage.setItem(storageKey, next)
        setThemeState(next)
      },
    }),
    [theme],
  )

  return <ThemeContext value={value}>{children}</ThemeContext>
}
