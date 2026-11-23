export const themeStorageKey = "astra-theme";

export const initializeTheme = `try {
  document.documentElement.classList.toggle('dark', localStorage.getItem('${themeStorageKey}') === 'dark');
} catch {}`;
