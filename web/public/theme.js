const theme = localStorage.getItem("intellyrouter-theme")
const dark =
  theme === "dark" || (theme !== "light" && window.matchMedia("(prefers-color-scheme: dark)").matches)
document.documentElement.classList.toggle("dark", dark)
document.documentElement.style.colorScheme = dark ? "dark" : "light"
