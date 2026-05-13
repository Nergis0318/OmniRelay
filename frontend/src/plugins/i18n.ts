import { createI18n } from "vue-i18n";
import en from "../locales/en";
import ko from "../locales/ko";

export type SupportedLocale = "en" | "ko";

const STORAGE_KEY = "omnirelay-locale";

function getSavedLocale(): SupportedLocale {
  const saved = localStorage.getItem(STORAGE_KEY);
  if (saved === "en" || saved === "ko") return saved;
  return "en";
}

const i18n = createI18n({
  legacy: false,
  locale: getSavedLocale() as string,
  fallbackLocale: "en" as string,
  messages: { en, ko },
});

export default i18n;

export function setLocale(locale: string) {
  (i18n.global.locale as any).value = locale;
  localStorage.setItem(STORAGE_KEY, locale);
}
