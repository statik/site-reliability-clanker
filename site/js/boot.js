// Runs before the first paint: marks that JavaScript works and applies the saved theme.
// `?theme=light|dark` overrides the saved choice (the capture scripts use it).
(function () {
  var root = document.documentElement;
  root.classList.add("js");
  var saved = null;
  try {
    saved = window.localStorage.getItem("clanker-theme");
  } catch (error) {
    saved = null; // Storage can be blocked. The system setting still applies.
  }
  var asked = new URLSearchParams(window.location.search).get("theme");
  if (asked === "light" || asked === "dark") saved = asked;
  if (saved === "light" || saved === "dark") root.dataset.theme = saved;
  var dark = saved ? saved === "dark" : window.matchMedia("(prefers-color-scheme: dark)").matches;
  root.dataset.resolvedTheme = dark ? "dark" : "light";
})();
