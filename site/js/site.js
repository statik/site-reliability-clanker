// Shared behaviour for every page: the theme toggle and the reveal-on-scroll motion.

const root = document.documentElement;
const systemDark = window.matchMedia("(prefers-color-scheme: dark)");

export const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");

function resolvedTheme() {
  const chosen = root.dataset.theme;
  if (chosen === "light" || chosen === "dark") return chosen;
  return systemDark.matches ? "dark" : "light";
}

function applyTheme(button) {
  const theme = resolvedTheme();
  root.dataset.resolvedTheme = theme;
  if (button) {
    const next = theme === "dark" ? "light" : "dark";
    button.setAttribute("aria-label", `Switch to the ${next} theme`);
    button.title = `Switch to the ${next} theme`;
  }
  document.dispatchEvent(new CustomEvent("themechange", { detail: { theme } }));
}

function startThemeToggle() {
  const button = document.querySelector(".theme-toggle");
  applyTheme(button);
  systemDark.addEventListener("change", () => applyTheme(button));
  if (!button) return;
  button.addEventListener("click", () => {
    const next = resolvedTheme() === "dark" ? "light" : "dark";
    root.dataset.theme = next;
    try {
      window.localStorage.setItem("clanker-theme", next);
    } catch (error) {
      console.warn("theme: the choice could not be saved", error);
    }
    applyTheme(button);
  });
}

// Parts with the class `reveal` rise in when they first come into view. Siblings rise one
// after another. CSS skips the motion when the reader asked for reduced motion.
function startReveal() {
  const items = Array.from(document.querySelectorAll(".reveal"));
  const counters = new Map();
  for (const item of items) {
    const index = counters.get(item.parentElement) ?? 0;
    counters.set(item.parentElement, index + 1);
    item.style.setProperty("--i", String(Math.min(index, 8)));
  }
  if (!("IntersectionObserver" in window)) {
    items.forEach((item) => item.classList.add("is-seen"));
    return;
  }
  const observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        entry.target.classList.add("is-seen");
        observer.unobserve(entry.target);
      }
    },
    { rootMargin: "0px 0px -8% 0px" },
  );
  items.forEach((item) => observer.observe(item));
}

startThemeToggle();
startReveal();
