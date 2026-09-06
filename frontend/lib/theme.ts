export type ThemeMode = 'dark' | 'light'

export const THEME_STORAGE_KEY = 'vidlens-theme'

export function readStoredTheme(): ThemeMode {
  if (typeof window === 'undefined') return 'dark'
  try {
    const v = window.localStorage.getItem(THEME_STORAGE_KEY)
    if (v === 'light' || v === 'dark') return v
  } catch { /* private mode */ }
  return 'dark'
}

export function applyTheme(mode: ThemeMode) {
  if (typeof document === 'undefined') return
  document.documentElement.setAttribute('data-theme', mode)
  try {
    window.localStorage.setItem(THEME_STORAGE_KEY, mode)
  } catch { /* private mode */ }
}
