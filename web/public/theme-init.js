// Sets the light/dark theme before the app renders (no flash). Same key as src/lib/theme.tsx.
(function () {
  var p = null
  try { p = localStorage.getItem('setu-theme') || localStorage.getItem('setu-theme-def') } catch (e) { /* private mode */ }
  if (p !== 'light' && p !== 'dark') p = 'system'
  var dark = p === 'dark' || (p === 'system' && window.matchMedia && matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.setAttribute('data-theme', dark ? 'dark' : 'light')
})()
