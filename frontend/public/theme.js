// Applies the stored theme (src/theme.ts) before the stylesheet and the first
// paint, so a dark choice never flashes light. A file, not an inline script,
// so the CSP in deploy/nginx.conf can keep script-src 'self'.
try {
  var stored = localStorage.getItem('theme')
  if (stored === 'light' || stored === 'dark') document.documentElement.dataset.theme = stored
} catch (e) {
  /* Storage disabled: fall back to prefers-color-scheme. */
}
