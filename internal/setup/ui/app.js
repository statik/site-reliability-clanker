// The first-run wizard. The server holds the state; this file draws the current step.

const TOKEN_KEY = "clanker.setup.token";
const fromUrl = new URLSearchParams(window.location.search).get("token");
if (fromUrl) {
  window.sessionStorage.setItem(TOKEN_KEY, fromUrl);
  window.history.replaceState(null, "", window.location.pathname);
}
const token = window.sessionStorage.getItem(TOKEN_KEY) ?? "";

const main = document.getElementById("main");
const stepList = document.getElementById("steps");

const STEPS = [
  { id: "target", title: "Where to run" },
  { id: "checks", title: "Check this machine" },
  { id: "key", title: "Language model" },
  { id: "signin", title: "Sign in", only: "aws" },
  { id: "where", title: "Network", only: "aws" },
  { id: "details", title: "Names and tags", only: "aws" },
  { id: "review", title: "Review" },
  { id: "create", title: "Create" },
  { id: "handover", title: "Hand over", only: "local" },
  { id: "done", title: "Done", only: "aws" },
];

let view = null;
let step = "target";
let message = null;
let timer = null;
const form = { target: "", env: "clanker", operator: "", key: "", model: "", local: null, profile: "", profiles: null,
  checks: null, found: null, surveying: false, place: "", details: null, zone: "", tags: [], newTag: "",
  checkout: "", nanos: "", confirm: "" };

async function call(method, path, body) {
  const response = await fetch(path, {
    method,
    headers: { "X-Setup-Token": token, "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = new Error(data.error ?? `the request failed with HTTP ${response.status}`);
    error.status = response.status;
    throw error;
  }
  return data;
}

function el(tag, attributes = {}, ...children) {
  const node = document.createElement(tag);
  for (const [name, value] of Object.entries(attributes)) {
    if (name === "class") node.className = value;
    else if (name.startsWith("on")) node.addEventListener(name.slice(2), value);
    else if (value === true) node.setAttribute(name, "");
    else if (value !== false && value !== null && value !== undefined) node.setAttribute(name, value);
  }
  for (const child of children.flat()) {
    if (child === null || child === undefined || child === false) continue;
    node.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
  return node;
}

const visibleSteps = () => STEPS.filter((item) => !item.only || item.only === targetOf());
const targetOf = () => form.target || view?.state.target || "";
const jobRunning = () => view?.job?.status === "running";

function go(next) {
  step = next;
  message = null;
  draw();
}

function fail(error) {
  message = { kind: "error", text: error.message };
  draw();
}

async function refresh() {
  view = await call("GET", "/api/state");
  document.getElementById("version").textContent = view.version;
}

function watchJob(onDone) {
  window.clearInterval(timer);
  timer = window.setInterval(async () => {
    try {
      await refresh();
    } catch (error) {
      window.clearInterval(timer);
      fail(error);
      return;
    }
    if (!jobRunning()) {
      window.clearInterval(timer);
      if (view.job?.status === "done" && onDone) await onDone();
    }
    draw();
  }, 1500);
}

function drawSteps() {
  const list = visibleSteps();
  const current = list.findIndex((item) => item.id === step);
  stepList.replaceChildren(...list.map((item, index) => el("li", {
    class: index < current ? "is-done" : index === current ? "is-current" : "",
    "aria-current": index === current ? "step" : null,
  }, item.title)));
}

function notice() {
  if (!message) return null;
  return el("div", { class: `notice notice-${message.kind}`, role: "alert" }, message.text);
}

function jobBlock() {
  const job = view?.job;
  if (!job) return null;
  return el("div", { class: "block" },
    job.status === "running" ? el("p", {}, el("span", { class: "spinner" }), "Working…") : null,
    job.status === "running" && job.total > 0 ? el("p", { class: "progress" },
      el("progress", { value: job.completed, max: job.total, "aria-label": "Download" }),
      `${gigabytes(job.completed)} of ${gigabytes(job.total)} GB`) : null,
    job.lines.length > 0 ? el("div", { class: "log", tabindex: "0" }, job.lines.slice(-400).join("\n")) : null,
    job.status === "failed" ? el("div", { class: "notice notice-error", role: "alert" },
      el("strong", {}, "This step failed."), job.error) : null);
}

const gigabytes = (bytes) => (bytes / 1e9).toFixed(1);

function buttons(...items) {
  return el("div", { class: "btn-row" }, items);
}

function button(label, kind, onclick, disabled = false) {
  return el("button", { type: "button", class: `btn-${kind}`, onclick, disabled }, label);
}

// ---------------------------------------------------------------- 1. where to run

function choice(value, title, text, disabledNote) {
  const selected = form.target === value;
  return el("label", { class: `choice${selected ? " is-selected" : ""}${disabledNote ? " is-disabled" : ""}` },
    el("input", { type: "radio", name: "target", value, checked: selected, disabled: Boolean(disabledNote),
      onchange: () => { form.target = value; draw(); } }),
    el("span", { class: "choice-title" }, title, disabledNote ? el("span", { class: "tag" }, disabledNote) : null),
    el("span", { class: "choice-text" }, text));
}

function drawTarget() {
  return [
    el("h1", {}, "Where do you want S.R.C. to run?"),
    el("p", { class: "lead" }, "S.R.C. has two parts: the control plane, which finds the work and " +
      "asks you, and the environment that it looks after. This wizard prepares the home of the " +
      "control plane. The control plane then proposes the first deployment, and you approve it."),
    el("div", { class: "choices", role: "radiogroup", "aria-label": "Target" },
      choice("local", "This machine", "A Kubernetes cluster in Docker, with the example application. " +
        "Free. Good for a first look."),
      choice("aws", "Your AWS account", "A unikernel on one small virtual machine, in a private " +
        "network of your account, with a small PostgreSQL database. The wizard signs in with your " +
        "AWS command line, reads your account, shows what S.R.C. creates there and its price, and " +
        "creates it when you press Create."),
      choice("gcp", "Google Cloud", "The stacks exist and are validated. The wizard does not set " +
        "up this cloud yet.", "not yet"),
      choice("azure", "Azure", "The stacks exist and are validated. The wizard does not set up " +
        "this cloud yet.", "not yet")),
    form.target === "aws" ? el("label", { class: "field" }, "Name of the environment",
      el("input", { type: "text", value: form.env, autocomplete: "off", spellcheck: "false",
        oninput: (event) => { form.env = event.target.value; } }),
      el("span", { class: "help" }, "3 to 24 lowercase letters, digits or hyphens. It becomes part " +
        "of the names of the resources.")) : null,
    form.target === "local" ? el("label", { class: "field" }, "Your name",
      el("input", { type: "text", value: form.operator, autocomplete: "name", maxlength: "60",
        oninput: (event) => { form.operator = event.target.value; } }),
      el("span", { class: "help" }, "The dashboard shows this name when you approve a change. " +
        "It stays on this machine.")) : null,
    notice(),
    buttons(button("Continue", "primary", async () => {
      try {
        await call("POST", "/api/target",
          { target: form.target, env: form.env, operator: form.operator });
        form.checks = null;
        await refresh();
        go("checks");
      } catch (error) { fail(error); }
    }, form.target !== "local" && form.target !== "aws")),
  ];
}

// ---------------------------------------------------------------- 2. checks

async function loadChecks() {
  try {
    form.checks = await call("GET", `/api/checks?target=${targetOf()}`);
  } catch (error) { fail(error); return; }
  draw();
}

function drawChecks() {
  if (form.checks === null) {
    loadChecks();
    return [el("h1", {}, "Checking this machine"), el("p", {}, el("span", { class: "spinner" }), "Looking…")];
  }
  const { checks, ready } = form.checks;
  return [
    el("h1", {}, ready ? "This machine is ready." : "This machine needs a few things first."),
    el("p", { class: "lead" }, targetOf() === "aws"
      ? "For AWS, the wizard uses the AWS command line with your own sign-in. It stores no credential."
      : "The local target runs everything in Docker on this machine."),
    el("table", {},
      el("thead", {}, el("tr", {}, el("th", {}, "Needed"), el("th", {}, "For"), el("th", {}, "Status"))),
      el("tbody", {}, checks.map((check) => el("tr", {},
        el("td", {}, el("code", {}, check.name)),
        el("td", {}, check.why),
        el("td", { class: check.ok ? "ok" : "bad" }, check.ok ? "Found" : `${check.detail}. `,
          check.ok ? null : el("code", {}, check.fix)))))),
    notice(),
    buttons(
      button("Back", "secondary", () => go("target")),
      ready ? null : button("Check again", "secondary", () => { form.checks = null; draw(); }),
      button("Continue", "primary", () => go("key"), !ready)),
  ];
}

// ---------------------------------------------------------------- 3. language model

const MODELS = {
  live: "A hosted key",
  local: "A model on this machine",
};
const LOCAL_LIMITS = "In our evals, this model explained changes and diagnosed incidents as well as " +
  "the hosted models. As the Author it scored 0.85 and 0.80 against 0.92 and 1.00, and as the " +
  "Reviewer it copied a secret into its answer in 1 of 15 runs. Use a hosted model for the Author " +
  "and the Reviewer when the environment matters.";

function modelChoice(value, text, disabledNote) {
  const selected = form.model === value;
  return el("label", { class: `choice${selected ? " is-selected" : ""}${disabledNote ? " is-disabled" : ""}` },
    el("input", { type: "radio", name: "model", value, checked: selected, disabled: Boolean(disabledNote),
      onchange: () => { form.model = value; message = null; draw(); } }),
    el("span", { class: "choice-title" }, MODELS[value]),
    [text].flat().map((part) => el("span", { class: "choice-text" }, part)),
    disabledNote ? el("span", { class: "choice-text" }, disabledNote) : null);
}

async function loadLocalModel() {
  try {
    form.local = await call("GET", "/api/model/local");
  } catch (error) { fail(error); return; }
  draw();
}

function localModelBlock() {
  if (form.local === null) {
    loadLocalModel();
    return el("p", { class: "block" }, el("span", { class: "spinner" }), "Checking this machine…");
  }
  return [
    el("h2", {}, "What the model on this machine needs"),
    el("table", {},
      el("thead", {}, el("tr", {}, el("th", {}, "Needed"), el("th", {}, "For"), el("th", {}, "Status"))),
      el("tbody", {}, form.local.checks.map((check) => el("tr", {},
        el("td", {}, check.name),
        el("td", {}, check.why),
        el("td", { class: check.ok ? "ok" : "bad" }, check.ok ? check.detail : `${check.detail}. ${check.fix}`))))),
    form.local.present ? null : el("p", { class: "help" }, `The wizard downloads ${form.local.model} through ` +
      "Ollama. A download that stops continues where it stopped."),
    view.job?.name === "model" ? jobBlock() : null,
  ];
}

function hostedKeyBlock() {
  const where = targetOf() === "aws"
    ? "The wizard holds it in memory. Create writes it to home.tfvars on this machine, readable " +
      "only by you, and to an encrypted parameter in your AWS account that only the control plane " +
      "can read."
    : "The wizard saves it in the data directory on this machine, readable only by you.";
  return el("label", { class: "field" }, "OpenRouter API key",
    el("input", { type: "password", autocomplete: "off", spellcheck: "false", placeholder: "sk-or-…",
      value: form.key, oninput: (event) => { form.key = event.target.value; } }),
    el("span", { class: "help" }, where));
}

function modelButton() {
  const busy = jobRunning();
  if (form.model === "live") return button("Save the key and continue", "primary", saveModel, busy);
  if (form.model !== "local") return button("Continue", "primary", saveModel, true);
  if (form.local === null || !form.local.ready) {
    return button("Check again", "secondary", () => { form.local = null; draw(); }, form.local === null);
  }
  return button(form.local.present ? "Use this model and continue" : "Download the model and continue",
    "primary", saveModel, busy);
}

function drawKey() {
  const aws = targetOf() === "aws";
  if (form.model === "") form.model = view.state.key_choice;
  if (aws && form.model === "local") form.model = "";
  return [
    el("h1", {}, "Choose the language model of S.R.C."),
    el("p", { class: "lead" }, "The model drafts changes and writes the summaries. It gets no cloud " +
      "credential."),
    el("div", { class: "choices choices-list", role: "radiogroup", "aria-label": "Language model" },
      modelChoice("live", "An OpenRouter key. S.R.C. chooses a hosted model for each role, and stops " +
        "at 5 USD each day."),
      modelChoice("local", ["Ollama runs qwen3.8:27b on this machine. It needs no key, and no text " +
        "leaves the machine. It needs 32 GB of memory and about 18 GB of disk.", LOCAL_LIMITS],
        aws ? "A control plane on AWS cannot reach a model on this machine." : null)),
    form.model === "live" ? hostedKeyBlock() : null,
    form.model === "local" ? localModelBlock() : null,
    notice(),
    buttons(button("Back", "secondary", () => go("checks"), jobRunning()), modelButton()),
  ];
}

const afterModel = () => go(targetOf() === "aws" ? "signin" : "review");

async function saveModel() {
  try {
    const { started } = await call("POST", "/api/model", { choice: form.model, key: form.key });
    form.key = "";
    await refresh();
    if (!started) { afterModel(); return; }
    draw();
    watchJob(async () => { form.local = null; afterModel(); });
  } catch (error) { fail(error); }
}

function modelFact() {
  return el("dl", { class: "facts" },
    el("div", {}, el("dt", {}, "Language model"), el("dd", {}, MODELS[view.state.key_choice] ?? "Not chosen")));
}

// ---------------------------------------------------------------- 4. sign in

async function loadProfiles() {
  try {
    form.profiles = (await call("GET", "/api/aws/profiles")).profiles;
    form.profile = view.state.profile || form.profiles[0] || "";
  } catch (error) { form.profiles = []; fail(error); return; }
  draw();
}

async function signIn() {
  const body = { profile: form.profile, region: "" };
  try {
    await call("POST", "/api/aws/signin", body);
    await refresh();
    form.found = null;
    message = null;
    draw();
  } catch (error) {
    if (error.status !== 409) { fail(error); return; }
    try {
      await call("POST", "/api/aws/login", body);
      await refresh();
      draw();
      watchJob(signIn);
    } catch (loginError) { fail(loginError); }
  }
}

function drawSignIn() {
  if (form.profiles === null) {
    loadProfiles();
    return [el("h1", {}, "Sign in to AWS"), el("p", {}, el("span", { class: "spinner" }), "Reading your profiles…")];
  }
  const state = view.state;
  const signedIn = Boolean(state.account) && state.profile === form.profile;
  return [
    el("h1", {}, "Sign in to your AWS account."),
    el("p", { class: "lead" }, "Choose a profile of the AWS command line. The wizard works as you, " +
      "with your permissions, and only while it runs. The next step reads your account and " +
      "changes nothing."),
    form.profiles.length === 0 ? el("div", { class: "notice notice-warning" },
      "The AWS command line has no profile on this machine. Run ", el("code", {}, "aws configure sso"),
      " in a terminal, then load this page again.") : null,
    el("label", { class: "field" }, "Profile",
      el("select", { onchange: (event) => { form.profile = event.target.value; draw(); } },
        form.profiles.map((name) => el("option", { value: name, selected: name === form.profile }, name)))),
    signedIn ? el("dl", { class: "facts" },
      el("div", {}, el("dt", {}, "Account"), el("dd", {}, state.account)),
      el("div", {}, el("dt", {}, "Signed in as"), el("dd", {}, state.principal.split("/").slice(-1)[0]))) : null,
    view.job?.name === "login" ? jobBlock() : null,
    notice(),
    buttons(
      button("Back", "secondary", () => go("key")),
      button(signedIn ? "Sign in again" : "Sign in", signedIn ? "secondary" : "primary", signIn,
        jobRunning() || form.profile === ""),
      button("Continue", "primary", () => go("where"), !signedIn || jobRunning())),
  ];
}

// ---------------------------------------------------------------- 5. network

const NEW_NETWORK = "new";

async function survey(region) {
  form.surveying = true;
  message = null;
  draw();
  try {
    form.found = await call("POST", "/api/aws/discover", { profile: view.state.profile, region });
    const proposal = form.found.placement;
    form.place = proposal.mode === "existing" ? proposal.network_id : NEW_NETWORK;
  } catch (error) {
    form.found = form.found ?? { failed: true };
    message = { kind: "error", text: error.message };
  }
  form.surveying = false;
  draw();
}

async function savePlace() {
  const body = form.place === NEW_NETWORK ? { mode: "new" } : { mode: "existing", network: form.place };
  try {
    await call("POST", "/api/aws/place", body);
    await refresh();
    form.details = null;
    go("details");
  } catch (error) { fail(error); }
}

function zonesOf(network) {
  return new Set(network.subnets.filter((subnet) => subnet.outbound === "nat").map((subnet) => subnet.zone)).size;
}

function accessOf(network) {
  if (!network.router) return "No Tailscale subnet router";
  return network.router.covers
    ? `Tailscale subnet router ${network.router.service}`
    : `Tailscale subnet router ${network.router.service}, which serves only a part of it`;
}

function networkChoice(network, found) {
  const rejected = (found.placement.rejected ?? []).find((item) => item.network_id === network.id);
  const proposed = found.placement.network_id === network.id;
  const selected = form.place === network.id;
  const tag = rejected ? "does not fit" : proposed ? "proposed" : null;
  const tags = Object.entries(network.tags ?? {}).filter(([key]) => key !== "Name" && !key.startsWith("aws:"))
    .map(([key, value]) => `${key}: ${value}`).join(", ");
  const facts = `${network.cidr}. ${network.instances} running instances. Private subnets with a NAT ` +
    `gateway in ${zonesOf(network)} availability zones. ${accessOf(network)}.` +
    (tags === "" ? "" : ` Tags: ${tags}.`);
  return el("label", { class: `choice${selected ? " is-selected" : ""}${rejected ? " is-disabled" : ""}` },
    el("input", { type: "radio", name: "network", value: network.id, checked: selected,
      disabled: Boolean(rejected), onchange: () => { form.place = network.id; draw(); } }),
    el("span", { class: "choice-title" }, network.name || network.id, tag ? el("span", { class: "tag" }, tag) : null),
    el("span", { class: "choice-text" }, facts, rejected ? ` It does not fit because ${rejected.reason}.` : ""));
}

function newNetworkChoice(found) {
  const free = found.new_network_cidr !== "";
  const proposed = found.placement.mode === "new";
  const selected = form.place === NEW_NETWORK;
  const tag = !free ? "no free range" : proposed ? "proposed" : null;
  return el("label", { class: `choice choice-wide${selected ? " is-selected" : ""}${free ? "" : " is-disabled"}` },
    el("input", { type: "radio", name: "network", value: NEW_NETWORK, checked: selected, disabled: !free,
      onchange: () => { form.place = NEW_NETWORK; draw(); } }),
    el("span", { class: "choice-title" }, "A new network", tag ? el("span", { class: "tag" }, tag) : null),
    el("span", { class: "choice-text" }, free
      ? `${found.new_network_cidr}, which overlaps no network of ${found.region}. It has private subnets ` +
        "and its own NAT gateway, which costs more than a network that exists. You then need a private " +
        "path to it, because the control plane has no public address."
      : `No address range between 10.64.0.0/22 and 10.255.0.0/22 is free in ${found.region}.`));
}

function rightsBlock(found) {
  if (found.rights_error) {
    return el("div", { class: "notice notice-warning" }, el("strong", {}, "Your permissions are not known."),
      `The policy simulator did not answer: ${found.rights_error}`);
  }
  const refused = (found.rights ?? []).filter((right) => !right.allowed);
  if (refused.length === 0) {
    return el("p", { class: "help" }, `Your sign-in can make the ${found.rights.length} kinds of call that the ` +
      "installation needs, by the policy simulator of IAM. A rule of your organization can still refuse one.");
  }
  return el("div", { class: "notice notice-warning" }, el("strong", {}, "Your sign-in cannot make these calls."),
    refused.map((right) => right.action).join(", "), ". The installation needs them.");
}

const holds = (region) => (region.instances > 0 || region.networks > 0
  ? `${region.instances} running instances, ${region.networks} networks` : "nothing runs here");

function regionField(found) {
  return el("label", { class: "field" }, "Region",
    el("select", { disabled: form.surveying, onchange: (event) => survey(event.target.value) },
      found.regions.map((region) => el("option", { value: region.name, selected: region.name === found.region },
        `${region.name}: ${holds(region)}`))),
    el("span", { class: "help" }, "The regions that are enabled in your account, the most used first."));
}

function drawWhere() {
  if (form.found === null) {
    if (!form.surveying) survey("");
    return [el("h1", {}, "Looking at your AWS account"),
      el("p", {}, el("span", { class: "spinner" }), "Reading the regions, the networks and your permissions. " +
        "This takes about half a minute. It changes nothing.")];
  }
  const found = form.found;
  if (found.failed) {
    return [el("h1", {}, "The wizard could not read your AWS account."), notice(),
      buttons(button("Back", "secondary", () => go("signin")),
        button("Try again", "primary", () => { form.found = null; draw(); }))];
  }
  return [
    el("h1", {}, "Choose the network of the control plane."),
    el("p", { class: "lead" }, "The control plane has no public address. It needs private subnets with a " +
      "NAT gateway in two availability zones, and a private path for you to reach it. The wizard read " +
      "your account and proposes a network. It changed nothing."),
    regionField(found),
    form.surveying ? el("p", {}, el("span", { class: "spinner" }), "Reading the region…") : null,
    el("div", { class: "choices", role: "radiogroup", "aria-label": "Network" },
      found.networks.map((network) => networkChoice(network, found)), newNetworkChoice(found)),
    el("h2", {}, "Why the wizard proposes this"),
    el("ul", {}, found.placement.reasons.map((reason) => el("li", {}, reason))),
    rightsBlock(found),
    notice(),
    buttons(
      button("Back", "secondary", () => go("signin")),
      button("Read the account again", "secondary", () => survey(found.region), form.surveying),
      button(form.place === NEW_NETWORK ? "Use a new network" : "Use this network", "primary", savePlace,
        form.surveying || form.place === "")),
  ];
}

// ---------------------------------------------------------------- 6. names and tags

async function loadDetails() {
  try {
    const details = await call("GET", "/api/aws/details");
    const state = view.state;
    const delegated = details.zones.filter((zone) => zone.delegated);
    form.zone = [state.zone_id, details.saved_zone].find((id) => delegated.some((zone) => zone.id === id)) ??
      delegated[0]?.id ?? "";
    const keys = new Set([...(details.launch.required_tags ?? []), ...details.saved_tag_keys, ...(state.tag_keys ?? [])]);
    form.tags = [...keys].sort().map((key) => ({ key, value: "" }));
    form.checkout = details.checkout;
    form.nanos = details.nanos;
    form.details = details;
  } catch (error) { form.details = { failed: true }; fail(error); return; }
  draw();
}

function zoneChoice(zone) {
  const selected = form.zone === zone.id;
  return el("label", { class: `choice${selected ? " is-selected" : ""}${zone.delegated ? "" : " is-disabled"}` },
    el("input", { type: "radio", name: "zone", value: zone.id, checked: selected, disabled: !zone.delegated,
      onchange: () => { form.zone = zone.id; draw(); } }),
    el("span", { class: "choice-title" }, zone.name, zone.delegated ? null : el("span", { class: "tag" }, "not delegated")),
    el("span", { class: "choice-text" }, `${zone.id}. `, zone.delegated
      ? `The control plane gets src-${view.state.env}.${zone.name} and Keycloak src-${view.state.env}-idp.${zone.name}.`
      : `It cannot hold the names: ${zone.reason}.`));
}

function launchText(details) {
  if (details.launch_error) return `The dry run of a launch did not answer: ${details.launch_error}. Add the tags that your organization requires.`;
  const launch = details.launch;
  if (launch.allowed) return "A dry run of a launch passed without tags: your organization requires none on a launch.";
  if (launch.required_tags.length > 0) {
    return `A dry run of a launch was refused. The rule names the tags ${launch.required_tags.join(", ")}. Give their values.`;
  }
  return `A dry run of a launch was refused: ${launch.reason}. Add the tags that your organization requires.`;
}

function tagRow(tag, index, saved) {
  return el("div", { class: "tag-row" },
    el("label", { class: "field" }, tag.key,
      el("input", { type: "text", value: tag.value, autocomplete: "off", spellcheck: "false",
        placeholder: saved.includes(tag.key) ? "keep the value of home.tfvars" : "value",
        oninput: (event) => { form.tags[index].value = event.target.value; } })),
    button("Remove the tag", "secondary", () => { form.tags.splice(index, 1); draw(); }));
}

function drawDetails() {
  if (form.details === null) {
    loadDetails();
    return [el("h1", {}, "Looking at your zones and tags"),
      el("p", {}, el("span", { class: "spinner" }), "Reading the public zones and making a dry run of a launch. " +
        "This changes nothing.")];
  }
  const details = form.details;
  if (details.failed) {
    return [el("h1", {}, "The wizard could not read your zones."), notice(),
      buttons(button("Back", "secondary", () => go("where")),
        button("Try again", "primary", () => { form.details = null; draw(); }))];
  }
  return [
    el("h1", {}, "Choose the names and the tags."),
    el("p", { class: "lead" }, "S.R.C. puts two names in a public zone of your account, with a certificate, and puts " +
      "the tags that your organization requires on each resource and on each launch. The wizard read your account " +
      "and changed nothing."),
    el("h2", {}, "Zone of the names"),
    details.zones_error ? el("div", { class: "notice notice-error" }, details.zones_error) : null,
    details.zones.length === 0 && !details.zones_error
      ? el("div", { class: "notice notice-warning" }, "The account has no public zone. Create one in Route 53 first.") : null,
    el("div", { class: "choices choices-list", role: "radiogroup", "aria-label": "Zone" }, details.zones.map(zoneChoice)),
    el("h2", {}, "Tags that your organization requires"),
    el("p", { class: "help" }, launchText(details), " The values stay on this machine and in your account: no log shows them."),
    form.tags.map((tag, index) => tagRow(tag, index, details.saved_tag_keys)),
    el("div", { class: "tag-row" },
      el("label", { class: "field" }, "Another tag",
        el("input", { type: "text", value: form.newTag, autocomplete: "off", spellcheck: "false", placeholder: "key",
          oninput: (event) => { form.newTag = event.target.value; } })),
      button("Add the tag", "secondary", () => {
        const key = form.newTag.trim();
        if (key !== "" && !form.tags.some((tag) => tag.key === key)) form.tags.push({ key, value: "" });
        form.newTag = "";
        draw();
      })),
    el("h2", {}, "What the installer builds"),
    el("label", { class: "field" }, "Checkout of S.R.C.",
      el("input", { type: "text", value: form.checkout, autocomplete: "off", spellcheck: "false",
        oninput: (event) => { form.checkout = event.target.value; } }),
      el("span", { class: "help" }, "The installer builds the runner image, the control plane and the images of the " +
        "application from this directory, for linux/arm64. A released program does not carry their source.")),
    el("label", { class: "field" }, "Release of the Nanos kernel",
      el("input", { type: "text", value: form.nanos, autocomplete: "off", spellcheck: "false",
        oninput: (event) => { form.nanos = event.target.value; } }),
      el("span", { class: "help" }, "The machine image of the control plane boots this kernel. 0.1.55 is the fallback " +
        "when an image does not boot.")),
    el("p", { class: "help" }, `The inputs go to ${details.directory}/home.tfvars, readable only by you.`),
    notice(),
    buttons(
      button("Back", "secondary", () => go("where")),
      button("Continue", "primary", saveDetails, form.zone === "")),
  ];
}

async function saveDetails() {
  const tags = Object.fromEntries(form.tags.map((tag) => [tag.key, tag.value]));
  try {
    await call("POST", "/api/aws/details", { zone_id: form.zone, tags, checkout: form.checkout, nanos: form.nanos });
    form.tags = form.tags.map((tag) => ({ key: tag.key, value: "" }));
    await refresh();
    go("review");
  } catch (error) { fail(error); }
}

// ---------------------------------------------------------------- 7. review

async function startReview() {
  try {
    await call("POST", "/api/review");
    await refresh();
    draw();
  } catch (error) { fail(error); }
}

const money = (value) => value.toFixed(2);

function drawReview() {
  if (targetOf() === "aws") return drawAwsReview();
  const reviewed = (view.review ?? []).length > 0;
  if (!reviewed && !message) startReview();
  return [
    el("h1", {}, reviewed ? "This is what the wizard will create." : "Working out what will be created"),
    el("p", { class: "lead" }, "The wizard starts the control plane on this machine. The control plane " +
      "then plans these parts and asks you before it creates them."),
    modelFact(),
    reviewed ? el("table", {},
      el("thead", {}, el("tr", {}, el("th", {}, "Part"), el("th", {}, "Type"))),
      el("tbody", {}, view.review.map((change) => el("tr", {},
        el("td", {}, change.id), el("td", {}, el("code", {}, change.type)))))) : null,
    notice(),
    buttons(
      button("Back", "secondary", () => go("key")),
      button("Start the control plane", "primary", startDeploy, !reviewed || jobRunning())),
  ];
}

function priceTable(plan) {
  return [
    el("h2", {}, "Price of one week"),
    el("table", {},
      el("thead", {}, el("tr", {}, el("th", {}, "Item"),
        el("th", { class: "num" }, "USD each hour"), el("th", { class: "num" }, "USD each week"))),
      el("tbody", {}, plan.prices.lines.map((line) => el("tr", {},
        el("td", {}, `${line.item}, `, el("code", {}, line.size)),
        el("td", { class: "num" }, String(line.hourly_usd)), el("td", { class: "num" }, money(line.weekly_usd))))),
      el("tfoot", {}, el("tr", {}, el("td", { colspan: "2" }, "One week, about"),
        el("td", { class: "num" }, money(plan.weekly_usd))))),
    el("p", { class: "help" }, `List prices of ${plan.prices.region}, read on ${plan.prices.checked}, with no tax ` +
      `and no data transfer. Not in the estimate: ${plan.not_priced.join("; ")}. AWS bills you, not this project.`),
  ];
}

function drawAwsReview() {
  const plan = view.aws_plan;
  if (!plan) {
    return [el("h1", {}, "The network is not chosen."),
      el("p", { class: "lead" }, "The review needs the sign-in and the network of the control plane."),
      buttons(button("Back", "secondary", () => go("where")))];
  }
  const reviewed = (view.review ?? []).length > 0;
  if (!reviewed && !message) startReview();
  const state = view.state;
  const tags = (state.tag_keys ?? []).length > 0 ? `${state.tag_keys.join(", ")} (values not shown)` : "None";
  return [
    el("h1", {}, "This is what S.R.C. creates in your AWS account."),
    el("p", { class: "lead" }, `${plan.network} ${plan.access}`),
    el("dl", { class: "facts" },
      el("div", {}, el("dt", {}, "Account"), el("dd", {}, state.account)),
      el("div", {}, el("dt", {}, "Region"), el("dd", {}, state.region)),
      el("div", {}, el("dt", {}, "Zone"), el("dd", {}, state.zone_name || "Not chosen")),
      el("div", {}, el("dt", {}, "Required tags"), el("dd", {}, tags)),
      el("div", {}, el("dt", {}, "Language model"), el("dd", {}, MODELS[state.key_choice] ?? "Not chosen")),
      el("div", {}, el("dt", {}, "Nanos kernel"), el("dd", {}, state.nanos || "Not chosen"))),
    el("table", {},
      el("thead", {}, el("tr", {}, el("th", {}, "Resource"), el("th", {}, "What it is"))),
      el("tbody", {}, plan.creates.map((item) => el("tr", {},
        el("td", {}, item.what), el("td", {}, item.detail))))),
    el("p", { class: "block" }, "Every resource carries the tag ", el("code", {}, `${plan.tag_key}=${plan.tag_value}`),
      ". Remove, on the last page, deletes everything that Create made and checks that nothing is left."),
    priceTable(plan),
    el("h2", {}, "What Create does"),
    el("ol", { class: "steps job-steps" }, view.install_steps.map((title) => el("li", {}, title))),
    el("p", { class: "help" }, "Each step can run again. When one fails, fix the cause and press Create again: it " +
      "goes on from that step. Nothing starts before you press Create."),
    plan.blocked.length > 0 ? el("div", { class: "notice notice-warning" },
      el("strong", {}, "Create cannot start yet."), plan.blocked.join("; "), ".") : null,
    notice(),
    buttons(
      button("Back", "secondary", () => go("details")),
      button("Create", "primary", startDeploy, !reviewed || plan.blocked.length > 0 || jobRunning())),
  ];
}

// ---------------------------------------------------------------- 8. create

async function startDeploy() {
  try {
    await call("POST", "/api/deploy");
    await refresh();
    go("create");
    watchJob(async () => go(targetOf() === "aws" ? "done" : "handover"));
  } catch (error) { fail(error); }
}

// jobSteps shows the steps of the installer or of the removal and how far the job is.
function jobSteps(job) {
  if (!job || job.steps.length === 0) return null;
  const done = job.status === "done";
  return el("ol", { class: "steps job-steps", "aria-label": "Progress" }, job.steps.map((title, index) => {
    let state = "";
    if (done || index < job.current) state = "is-done";
    else if (index === job.current) state = job.status === "failed" ? "is-failed" : "is-current";
    return el("li", { class: state }, title);
  }));
}

function drawCreate() {
  const failed = view.job?.status === "failed";
  if (targetOf() === "aws") {
    return [
      el("h1", {}, failed ? "The installation stopped." : "Creating S.R.C. in your AWS account"),
      el("p", { class: "lead" }, failed
        ? "Fix the cause that the log names, then press Create again. It goes on from the step that failed."
        : "This takes a while: the database, the images and the first start of the control plane take the " +
          "longest. The wizard goes on when you close this page."),
      jobSteps(view.job),
      jobBlock(),
      notice(),
      failed ? buttons(button("Back to the review", "secondary", () => go("review")),
        button("Create again", "primary", startDeploy, jobRunning())) : null,
      failed ? removeBlock("Or remove what Create made so far") : null,
    ];
  }
  return [
    el("h1", {}, failed ? "The start stopped." : "Starting the control plane"),
    el("p", { class: "lead" }, "This takes less than a minute."),
    jobBlock(),
    notice(),
    failed ? buttons(button("Back to the review", "secondary", () => go("review"))) : null,
  ];
}

// ---------------------------------------------------------------- 9. done and remove

function drawDone() {
  const result = view.state.result;
  if (!view.state.installed || !result) {
    return [el("h1", {}, "S.R.C. is not installed in your AWS account."),
      buttons(button("Back to the review", "secondary", () => go("review")))];
  }
  return [
    el("h1", {}, "S.R.C. runs in your AWS account."),
    el("p", { class: "lead" }, "The control plane plans the first deployment and waits for your approval. You reach it " +
      "through the Tailscale subnet router of the network."),
    el("dl", { class: "facts" },
      el("div", {}, el("dt", {}, "Dashboard"), el("dd", {}, el("a", { href: result.dashboard_url, target: "_blank",
        rel: "noopener" }, result.dashboard_url))),
      el("div", {}, el("dt", {}, "Sign in as"), el("dd", {}, result.admin_email)),
      el("div", {}, el("dt", {}, "Second approver"), el("dd", {}, result.approver_email)),
      el("div", {}, el("dt", {}, "Price of one week, about"), el("dd", {}, `${money(result.weekly_usd)} USD`))),
    el("h2", {}, "Passwords of Keycloak"),
    el("p", { class: "help" }, "The state of the home stack holds them. This wizard does not show them. Run this in a " +
      "terminal:"),
    el("pre", { class: "log" }, result.passwords),
    notice(),
    removeBlock("Remove"),
  ];
}

// removeBlock asks for the name of the environment before Remove. Typing changes only the
// button, so that the field keeps the focus.
function removeBlock(title) {
  const env = view.state.env;
  const remove = button(`Remove ${env}`, "danger", startRemove, form.confirm !== env || jobRunning());
  return [
    el("h2", {}, title),
    el("p", { class: "help" }, "Remove deletes everything that Create made, in the order of the teardown, and checks at " +
      `the end that nothing is left. Type ${env} to confirm.`),
    el("label", { class: "field" }, "Name of the environment",
      el("input", { type: "text", value: form.confirm, autocomplete: "off", spellcheck: "false",
        oninput: (event) => {
          form.confirm = event.target.value;
          remove.disabled = form.confirm !== env || jobRunning();
        } })),
    buttons(remove),
  ];
}

async function startRemove() {
  try {
    await call("POST", "/api/aws/remove", { env: form.confirm });
    form.confirm = "";
    await refresh();
    go("remove");
    watchJob();
  } catch (error) { fail(error); }
}

function drawRemove() {
  const status = view.job?.status;
  const title = { running: "Removing S.R.C. from your AWS account", failed: "The removal stopped.",
    done: "S.R.C. is removed from your AWS account." }[status] ?? "Removal";
  return [
    el("h1", {}, title),
    el("p", { class: "lead" }, status === "failed"
      ? "Fix the cause that the log names, then remove again. Each step checks before it reports."
      : "Each step checks that its part is gone before the next one starts."),
    jobSteps(view.job),
    jobBlock(),
    notice(),
    status === "failed" ? buttons(button("Remove again", "danger", async () => {
      form.confirm = view.state.env;
      await startRemove();
    }, jobRunning())) : null,
    status === "done" ? buttons(button("Back to the start", "secondary", () => go("target"))) : null,
  ];
}

// ---------------------------------------------------------------- 10. hand over

function drawHandover() {
  const up = view.dashboard_up;
  return [
    el("h1", {}, up ? "The control plane runs. It is yours now." : "The control plane does not answer here."),
    el("p", { class: "lead" }, "On a first run it plans the first deployment of the example application " +
      "and waits. Nothing is created until the approvers agree. The wizard does not approve for you."),
    modelFact(),
    view.job && view.job.name !== "deploy" ? jobBlock() : null,
    notice(),
    buttons(
      up ? el("a", { href: `${view.dashboard_url}/approvals`, target: "_blank", rel: "noopener" },
        button("Open the first approval", "primary", () => {})) : null,
      up ? null : button("Start the control plane", "primary", startDeploy, jobRunning())),
    view.in_app ? appHelp() : el("p", { class: "help" }, "To stop, press Ctrl+C in the terminal. To start again later, run ",
      el("code", {}, "clanker up"), ". To remove the local environment and its history, run ", el("code", {}, "clanker reset"), "."),
  ];
}

function appHelp() {
  return el("p", { class: "help" }, "To stop the control plane, quit this app. To start it again, open the app: " +
    "it starts the control plane and shows the dashboard. To remove the local environment and start again, " +
    "choose Reset Demo Environment in the S.R.C. menu.");
}

// In the app, a setup that is finished starts the control plane and shows the dashboard.
async function resume() {
  const showDashboard = () => window.location.assign(view.dashboard_url);
  if (view.dashboard_up) {
    showDashboard();
    return;
  }
  try {
    await call("POST", "/api/deploy");
    await refresh();
    go("create");
    watchJob(showDashboard);
  } catch (error) {
    draw();
    fail(error);
  }
}

const DRAW = { target: drawTarget, checks: drawChecks, key: drawKey, signin: drawSignIn, where: drawWhere,
  details: drawDetails, review: drawReview, create: drawCreate, handover: drawHandover, done: drawDone,
  remove: drawRemove };

function draw() {
  drawSteps();
  main.replaceChildren(...DRAW[step]().flat().filter(Boolean));
  // A log shows its last lines.
  for (const log of main.querySelectorAll("div.log")) log.scrollTop = log.scrollHeight;
}

async function start() {
  if (token === "") {
    main.replaceChildren(el("div", { class: "notice notice-error" },
      "This page needs the address that ", el("code", {}, "clanker setup"), " printed. It contains an access token."));
    return;
  }
  try {
    await refresh();
  } catch (error) {
    main.replaceChildren(el("div", { class: "notice notice-error" }, error.message));
    return;
  }
  form.target = view.state.target;
  form.env = view.state.env || form.env;
  form.operator = view.state.operator || form.operator;
  if (view.state.deployed && view.state.target === "local" && !view.state.key_choice) {
    // The saved choice is one that this version does not have.
    step = "key";
    message = { kind: "error", text: "Choose a language model. S.R.C. needs one to start " +
      "the control plane." };
  } else if (view.state.deployed && view.state.target === "local") {
    step = "handover";
    if (view.in_app && !jobRunning()) {
      await resume();
      return;
    }
  }
  if (view.state.target === "aws" && view.state.installed) step = "done";
  // A Create that failed shows its log; one that ended goes to Done.
  if (view.job?.name === "install" && view.job.status === "failed") step = "create";
  if (view.job?.name === "remove") step = "remove";
  if (jobRunning()) {
    step = { deploy: "create", model: "key", install: "create", remove: "remove" }[view.job.name] ?? step;
    watchJob(view.job.name === "install" ? async () => go("done") : undefined);
  }
  draw();
}

start();
