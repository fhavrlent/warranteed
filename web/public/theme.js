// Applies the saved theme before first paint. Kept tiny and external (the CSP allows no inline scripts);
// without it the page follows the system scheme.
try {
  document.querySelector('meta[name="color-scheme"]').content =
    localStorage.getItem("warranteed-theme") || "light dark";
} catch (e) {}
