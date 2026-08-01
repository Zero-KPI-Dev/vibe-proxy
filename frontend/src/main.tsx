import React from "react"
import ReactDOM from "react-dom/client"
import App from "./App"
import { AuthGate } from "@/components/auth-gate"
import "./index.css"
import "./i18n"

const theme = localStorage.getItem("vibe_theme") ?? "dark"
document.documentElement.classList.toggle("light", theme === "light")
document.documentElement.classList.toggle("dark", theme !== "light")

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <AuthGate>
      <App />
    </AuthGate>
  </React.StrictMode>
)
