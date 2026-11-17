"use strict";

const IN_TANK = ["available", "reserved", "quarantined"];
const RETRY_AFTER_ERROR_SECONDS = 3;

const pulse = document.getElementById("pulse");
const pulseText = document.getElementById("pulse-text");
const toast = document.getElementById("toast");
let toastTimer;

function setPulse(state, text) {
  pulse.dataset.state = state;
  pulseText.textContent = text;
}

function showToast(message, kind) {
  toast.textContent = message;
  toast.dataset.kind = kind;
  toast.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { toast.hidden = true; }, 5000);
}

function statusLabel(status) {
  const words = status.replaceAll("_", " ");
  return words.charAt(0).toUpperCase() + words.slice(1);
}

async function postJSON(path, body) {
  const response = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (response.ok) {
    return;
  }
  let message = `The request failed with status ${response.status}`;
  try {
    message = (await response.json()).error || message;
  } catch {
    // The body was not JSON; the status text above is the best message there is.
  }
  throw new Error(message);
}

/* Live region: fetch this page again and swap the part that changes. */

function live() {
  return document.getElementById("live");
}

async function refreshLive() {
  const region = live();
  if (!region) {
    return;
  }
  try {
    const response = await fetch(location.href, { headers: { Accept: "text/html" } });
    const fresh = new DOMParser().parseFromString(await response.text(), "text/html")
      .getElementById("live");
    if (!fresh) {
      throw new Error("the page has no live region");
    }
    const failed = fresh.querySelector("[data-load-error]");
    const haveData = !region.querySelector("[data-load-error]");
    if (failed && haveData) {
      setPulse("stale", "Showing the last good data");
      return;
    }
    region.innerHTML = fresh.innerHTML;
    if (!failed && !document.body.matches('[data-page="board"]')) {
      setPulse("live", "Up to date");
    }
  } catch {
    setPulse("down", "The server does not answer");
  }
}

function scheduleRefresh() {
  const region = live();
  if (!region) {
    return;
  }
  const failed = region.querySelector("[data-load-error]");
  const seconds = failed ? RETRY_AFTER_ERROR_SECONDS : Number(region.dataset.refresh);
  setTimeout(async () => {
    await refreshLive();
    scheduleRefresh();
  }, seconds * 1000);
}

/* Actions: buttons with data-post and forms with data-api-form talk to the API. */

document.addEventListener("click", async (event) => {
  const button = event.target.closest("button[data-post]");
  if (!button) {
    return;
  }
  button.disabled = true;
  try {
    const body = button.dataset.body ? JSON.parse(button.dataset.body) : undefined;
    await postJSON(button.dataset.post, body);
    showToast(`${button.textContent.trim()}: done`, "ok");
  } catch (error) {
    showToast(error.message, "error");
  }
  await refreshLive();
  button.disabled = false;
});

document.addEventListener("submit", async (event) => {
  const form = event.target.closest("form[data-api-form]");
  if (!form) {
    return;
  }
  event.preventDefault();
  const body = {};
  for (const field of form.elements) {
    if (!field.name || field.value === "") {
      continue;
    }
    body[field.name] = "number" in field.dataset ? Number(field.value) : field.value;
  }
  const submit = form.querySelector("button[type=submit]");
  submit.disabled = true;
  try {
    await postJSON(form.dataset.apiForm, body);
    location.reload();
  } catch (error) {
    showToast(error.message, "error");
    submit.disabled = false;
  }
});

/* Inventory board: apply each fish status event from the stream. */

function recount() {
  const rows = [...document.querySelectorAll(".fish-row")];
  for (const segment of document.querySelectorAll("#stockbar [data-status]")) {
    const count = rows.filter((row) => row.dataset.status === segment.dataset.status).length;
    segment.style.flexGrow = count;
    const label = document.querySelector(`[data-count="${segment.dataset.status}"]`);
    if (label) {
      label.textContent = count;
    }
  }
  for (const tank of document.querySelectorAll(".tank")) {
    const inTank = [...tank.querySelectorAll(".fish-row")]
      .filter((row) => IN_TANK.includes(row.dataset.status)).length;
    tank.querySelector("[data-occupancy]").textContent = inTank;
  }
}

function applyFishStatus(change) {
  const row = document.querySelector(`.fish-row[data-fish-id="${change.fish_id}"]`);
  if (!row) {
    return;
  }
  row.dataset.status = change.status;
  row.querySelector("[data-status-label]").textContent = statusLabel(change.status);
  row.classList.remove("changed");
  requestAnimationFrame(() => row.classList.add("changed"));
  recount();
}

function watchInventory() {
  const stream = new EventSource("/api/inventory/stream");
  stream.addEventListener("open", () => setPulse("live", "Live"));
  stream.addEventListener("error", () => setPulse("down", "Reconnecting to the stream"));
  stream.addEventListener("fish_status", (event) => {
    setPulse("live", "Live");
    applyFishStatus(JSON.parse(event.data));
  });
}

if (document.body.matches('[data-page="board"]')) {
  watchInventory();
} else {
  setPulse("live", "Up to date");
}
scheduleRefresh();
