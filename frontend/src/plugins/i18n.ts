import { createI18n } from "vue-i18n";
import en from "../locales/en";
import ja from "../locales/ja";
import ko from "../locales/ko";

export type SupportedLocale = "en" | "ja" | "ko";

const STORAGE_KEY = "omnirelay-locale";

function detectBrowserLocale(): SupportedLocale {
  const lang = navigator.language.toLowerCase();
  if (lang.startsWith("ja")) return "ja";
  if (lang.startsWith("ko")) return "ko";
  return "en";
}

function getSavedLocale(): SupportedLocale {
  const saved = localStorage.getItem(STORAGE_KEY);
  if (saved === "en" || saved === "ja" || saved === "ko") return saved;
  return detectBrowserLocale();
}

const i18n = createI18n({
  legacy: false,
  locale: getSavedLocale() as string,
  fallbackLocale: "en" as string,
  messages: { en, ja, ko },
});

export default i18n;

export function setLocale(locale: string) {
  (i18n.global.locale as any).value = locale;
  localStorage.setItem(STORAGE_KEY, locale);
}
