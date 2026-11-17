// Shared behaviour for every page: the reveal-on-scroll motion.

export const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");

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

startReveal();
