// Applies persisted density before app mount to avoid FOUC.
//
// Theme and accent are deliberately absent: the product is light-only with a
// black primary, and a stale "pp.theme=dark" or "pp.accent=#22c55e" left in
// someone's storage would repaint the app with no control on screen to undo it.
// Bundled via the main entry so we don't need an inline <script> in index.html
// (strict CSP forbids inline scripts).
try {
  const d = localStorage.getItem('pp.density')
  if (d) document.documentElement.setAttribute('data-density', d)
} catch {
  // ignore (private mode, etc.)
}
