// Theme for the server-rendered auth pages. Shares the "mf-theme" key with the
// SPA so the choice follows the person across login and the panel.
(() => {
  const KEY = "mf-theme";
  const root = document.documentElement;
  const mq = window.matchMedia("(prefers-color-scheme: dark)");

  function apply(theme) {
    const resolved = theme === "system" ? (mq.matches ? "dark" : "light") : theme;
    root.dataset.theme = resolved;
    root.style.colorScheme = resolved;
  }

  const stored = localStorage.getItem(KEY);
  apply(stored === "light" || stored === "dark" ? stored : "system");

  mq.addEventListener("change", () => {
    const current = localStorage.getItem(KEY);
    if (!current || current === "system") apply("system");
  });

  const button = document.getElementById("theme-toggle");
  if (button) {
    button.addEventListener("click", () => {
      const next = root.dataset.theme === "dark" ? "light" : "dark";
      localStorage.setItem(KEY, next);
      apply(next);
    });
  }
})();
