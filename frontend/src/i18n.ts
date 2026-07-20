import i18n from "i18next"
import { initReactI18next } from "react-i18next"
import en from "@/locales/en.json"
import zhCN from "@/locales/zh-CN.json"

export const LANGUAGE_STORAGE_KEY = "vibe_locale"
export const SUPPORTED_LANGUAGES = ["zh-CN", "en"] as const
export type AppLanguage = (typeof SUPPORTED_LANGUAGES)[number]

export function normalizeLanguage(language?: string | null): AppLanguage {
  return language?.toLowerCase().startsWith("zh") ? "zh-CN" : "en"
}

function detectInitialLanguage(): AppLanguage {
  const stored = localStorage.getItem(LANGUAGE_STORAGE_KEY)
  if (stored) return normalizeLanguage(stored)

  const preferred = navigator.languages?.[0] ?? navigator.language
  return normalizeLanguage(preferred)
}

const initialLanguage = detectInitialLanguage()

void i18n.use(initReactI18next).init({
  resources: {
    en: { translation: en },
    "zh-CN": { translation: zhCN },
  },
  lng: initialLanguage,
  fallbackLng: "en",
  interpolation: { escapeValue: false },
  returnNull: false,
})

document.documentElement.lang = initialLanguage

i18n.on("languageChanged", (language) => {
  document.documentElement.lang = normalizeLanguage(language)
})

export async function setAppLanguage(language: AppLanguage) {
  localStorage.setItem(LANGUAGE_STORAGE_KEY, language)
  await i18n.changeLanguage(language)
}

export default i18n
