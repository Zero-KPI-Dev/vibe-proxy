import { desktopApi } from "@/lib/api"

/**
 * Copy text in browsers and embedded WebViews.
 *
 * The async Clipboard API is not consistently available to locally served
 * desktop WebViews, so use the authenticated native bridge there and retain a
 * selection-based fallback for ordinary browsers.
 */
export async function copyText(value: string): Promise<boolean> {
  if (!value) return false

  if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(value)
      return true
    } catch {
      // Continue to the WebView-compatible fallback below.
    }
  }

  // Chromium/WebKit clipboard permissions are inconsistent inside native
  // WebViews. The authenticated local desktop bridge delegates to the OS
  // clipboard and is unavailable (409) in ordinary server deployments.
  try {
    const result = await desktopApi.copyText(value)
    if (result.copied) return true
  } catch {
    // Continue to the browser-only fallback below.
  }

  if (typeof document === "undefined" || !document.body) return false

  const textarea = document.createElement("textarea")
  textarea.value = value
  textarea.setAttribute("readonly", "")
  textarea.style.position = "fixed"
  textarea.style.left = "-9999px"
  textarea.style.top = "0"
  textarea.style.opacity = "0"

  const focused = document.activeElement instanceof HTMLElement
    ? document.activeElement
    : null

  document.body.appendChild(textarea)
  textarea.focus()
  textarea.select()
  textarea.setSelectionRange(0, textarea.value.length)

  try {
    return document.execCommand("copy")
  } catch {
    return false
  } finally {
    textarea.remove()
    focused?.focus()
  }
}
