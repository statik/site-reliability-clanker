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
  { id: "review", title: "Review" },
  { id: "create", title: "Create" },
  { id: "handover", title: "Hand over" },
];

let view = null;
let step = "target";
let message = null;
let timer = null;
const form = { target: "", env: "clanker", key: "", model: "", local: null, profile: "", size: "small", profiles: null,
  checks: null, found: null, surveying: false, place: "" };

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
    job.lines.length > 0 ? el("div", { class: "log", tabindex: "0" }, job.lines.join("\n")) : null,
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
  const awsNote = view.aws_enabled ? null : "not yet";
  return [
    el("h1", {}, "Where do you want S.R.C. to run?"),
    el("p", { class: "lead" }, "S.R.C. has two parts: the control plane, which finds the work and " +
      "asks you, and the environment that it looks after. This wizard creates the home of the " +
      "control plane. The control plane then proposes the first deployment, and you approve it."),
    el("div", { class: "choices", role: "radiogroup", "aria-label": "Target" },
      choice("local", "This machine", "A Kubernetes cluster in Docker, with the example application. " +
        "Free. Good for a first look."),
      choice("aws", "Your AWS account", view.survey_only
        ? "The wizard signs in with your AWS command line, reads your account and proposes the network of the control plane. This version creates nothing on AWS."
        : view.aws_enabled
        ? "One small container for the control plane on Amazon ECS with Fargate, in its own network, with no open port."
        : "The control plane for AWS is being built as a unikernel in an auto scaling group. The wizard does not set up this cloud yet.", awsNote),
      choice("gcp", "Google Cloud", "The stacks exist and are validated. The wizard does not set " +
        "up this cloud yet.", "not yet"),
      choice("azure", "Azure", "The stacks exist and are validated. The wizard does not set up " +
        "this cloud yet.", "not yet")),
    form.target === "aws" ? el("label", { class: "field" }, "Name of the environment",
      el("input", { type: "text", value: form.env, autocomplete: "off", spellcheck: "false",
        oninput: (event) => { form.env = event.target.value; } }),
      el("span", { class: "help" }, "3 to 24 lowercase letters, digits or hyphens. It becomes part " +
        "of the names of the resources.")) : null,
    notice(),
    buttons(button("Continue", "primary", async () => {
      try {
        await call("POST", "/api/target", { target: form.target, env: form.env });
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
  recorded: "Recorded answers",
};
const LOCAL_LIMITS = "In our evals, this model explained changes and diagnosed incidents as well as " +
  "the hosted models. As the Author it scored 0.85 and 0.80 against 0.92 and 1.00, and as the " +
  "Reviewer it copied a secret into its answer in 5 of 12 runs. Use a hosted model for the Author " +
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
    ? "The wizard stores it encrypted in AWS Systems Manager Parameter Store in your account. " +
      "Only the control plane can read it. It is not written to a file here."
    : "The wizard saves it in the data directory on this machine, readable only by you.";
  return el("label", { class: "field" }, "OpenRouter API key",
    el("input", { type: "password", autocomplete: "off", spellcheck: "false", placeholder: "sk-or-…",
      value: form.key, oninput: (event) => { form.key = event.target.value; } }),
    el("span", { class: "help" }, where));
}

function modelButton() {
  const busy = jobRunning();
  if (form.model === "live") return button("Save the key and continue", "primary", saveModel, busy);
  if (form.model === "recorded") return button("Use recorded answers and continue", "primary", saveModel, busy);
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
        aws ? "A control plane on AWS cannot reach a model on this machine." : null),
      modelChoice("recorded", "S.R.C. answers from recordings, with no model and no key. The " +
        "recordings fit the example application only.")),
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
  const body = { profile: form.profile, region: "", size: form.size };
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
      "with your permissions, and only while it runs. " + (view.survey_only
      ? "The next step reads your account and changes nothing."
      : "You need permission to create a network, IAM roles, a file system and an ECS service.")),
    form.profiles.length === 0 ? el("div", { class: "notice notice-warning" },
      "The AWS command line has no profile on this machine. Run ", el("code", {}, "aws configure sso"),
      " in a terminal, then load this page again.") : null,
    el("label", { class: "field" }, "Profile",
      el("select", { onchange: (event) => { form.profile = event.target.value; draw(); } },
        form.profiles.map((name) => el("option", { value: name, selected: name === form.profile }, name)))),
    view.survey_only ? null : el("label", { class: "field" }, "Size of the container",
      el("select", { onchange: (event) => { form.size = event.target.value; } },
        [["small", "Small: 0.5 vCPU and 1 GB of memory, about 14 USD each month"],
          ["medium", "Medium: 1 vCPU and 2 GB of memory, about 29 USD each month"]].map(([value, label]) =>
          el("option", { value, selected: value === form.size }, label)))),
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
    if (view.survey_only) {
      message = { kind: "info", text: "The choice is saved. This version of the wizard stops here: " +
        "it does not create the control plane on AWS yet." };
      draw();
      return;
    }
    go("review");
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

function regionField(found) {
  const holds = (region) => (region.instances > 0 || region.networks > 0
    ? `${region.instances} running instances, ${region.networks} networks` : "nothing runs here");
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

// ---------------------------------------------------------------- 6. review

async function startReview() {
  try {
    const { started } = await call("POST", "/api/review");
    await refresh();
    draw();
    if (started) watchJob();
  } catch (error) { fail(error); }
}

const money = (value) => (value === 0 ? "0" : value.toFixed(2));

function drawReview() {
  const aws = targetOf() === "aws";
  const reviewed = (view.review ?? []).length > 0;
  const reviewJob = view.job?.name === "review" ? view.job : null;
  if (!reviewed && !reviewJob && !jobRunning()) startReview();
  const total = view.cost.reduce((sum, line) => sum + line.monthly_usd, 0);
  return [
    el("h1", {}, reviewed ? "This is what the wizard will create." : "Working out what will be created"),
    el("p", { class: "lead" }, aws
      ? "CloudFormation made this list from the template. Nothing exists yet. The next step creates exactly this."
      : "The wizard starts the control plane on this machine. The control plane then plans these parts and asks you before it creates them."),
    modelFact(),
    view.job?.name === "review" && !reviewed ? jobBlock() : null,
    reviewed ? el("table", {},
      el("thead", {}, el("tr", {}, el("th", {}, aws ? "Resource" : "Part"), el("th", {}, "Type"))),
      el("tbody", {}, view.review.map((change) => el("tr", {},
        el("td", {}, change.id), el("td", {}, el("code", {}, change.type)))))) : null,
    aws ? el("h2", {}, "Estimated cost") : null,
    aws ? el("table", {},
      el("thead", {}, el("tr", {}, el("th", {}, "Item"), el("th", {}, "Size"),
        el("th", { class: "num" }, "USD each month"))),
      el("tbody", {}, view.cost.map((line) => el("tr", {},
        el("td", {}, line.item), el("td", {}, line.size), el("td", { class: "num" }, money(line.monthly_usd))))),
      el("tfoot", {}, el("tr", {}, el("td", { colspan: "2" }, "Total, about"),
        el("td", { class: "num" }, money(total))))) : null,
    aws ? el("p", { class: "help" }, "List prices for us-east-1, with no tax and no data transfer. " +
      "AWS bills you, not this project. The environment that the control plane later proposes " +
      "(cluster, database, load balancer) costs more, and you approve it separately.") : null,
    aws ? el("div", { class: "notice" }, el("strong", {}, "How you reach it"),
      "The container has a public address, because it must reach AWS and the model provider, " +
      "but its firewall has no inbound rule. You reach the dashboard through a private tunnel of " +
      "ECS Exec, as the signed-in user.") : null,
    notice(),
    buttons(
      button("Back", "secondary", () => go(aws ? "where" : "key")),
      reviewJob?.status === "failed" ? button("Try the review again", "secondary", startReview) : null,
      button(aws ? "Create these resources" : "Start the control plane", "primary", startDeploy,
        !reviewed || jobRunning())),
  ];
}

// ---------------------------------------------------------------- 7. create

async function startDeploy() {
  try {
    await call("POST", "/api/deploy");
    await refresh();
    go("create");
    watchJob(async () => go("handover"));
  } catch (error) { fail(error); }
}

function drawCreate() {
  const failed = view.job?.status === "failed";
  return [
    el("h1", {}, failed ? "The creation stopped." : "Creating the home of the control plane"),
    el("p", { class: "lead" }, targetOf() === "aws"
      ? "This takes about five minutes. CloudFormation reports the service as created only when the container runs and passes its health check."
      : "This takes less than a minute."),
    jobBlock(),
    notice(),
    failed ? buttons(
      button("Back to the review", "secondary", () => go("review")),
      targetOf() === "aws" ? button("Remove what was created", "danger", startDestroy) : null) : null,
  ];
}

// ---------------------------------------------------------------- 8. hand over

async function startDestroy() {
  if (!window.confirm("Delete the control plane and everything that this wizard created in the account?")) return;
  try {
    await call("POST", "/api/destroy");
    await refresh();
    draw();
    watchJob(async () => { await refresh(); go("target"); });
  } catch (error) { fail(error); }
}

async function reconnect() {
  try {
    await call("POST", "/api/connect");
    await refresh();
    draw();
    watchJob();
  } catch (error) { fail(error); }
}

function drawHandover() {
  const aws = targetOf() === "aws";
  const up = view.dashboard_up;
  return [
    el("h1", {}, up ? "The control plane runs. It is yours now." : "The control plane does not answer here."),
    aws
      ? el("p", { class: "lead" }, "It runs in your AWS account. The dashboard below comes through the private tunnel, which lives while this program runs.")
      : el("p", { class: "lead" }, "On a first run it plans the first deployment of the example application and waits. Nothing is created until the approvers agree. The wizard does not approve for you."),
    aws ? el("div", { class: "notice notice-warning" }, el("strong", {}, "What does not work yet on AWS"),
      "The control plane starts and shows its dashboard. It does not install the environment on AWS " +
      "yet: that step is built and proven only on the local target.") : null,
    aws ? el("dl", { class: "facts" },
      el("div", {}, el("dt", {}, "Account"), el("dd", {}, view.state.account)),
      el("div", {}, el("dt", {}, "Region"), el("dd", {}, view.state.region)),
      el("div", {}, el("dt", {}, "ECS cluster"), el("dd", {}, `clanker-${view.state.env}-control`))) : null,
    aws ? null : modelFact(),
    view.job && view.job.name !== "deploy" ? jobBlock() : null,
    notice(),
    buttons(
      up ? el("a", { href: aws ? view.dashboard_url : `${view.dashboard_url}/approvals`, target: "_blank", rel: "noopener" },
        button(aws ? "Open the dashboard" : "Open the first approval", "primary", () => {})) : null,
      aws && !up ? button("Open the tunnel again", "primary", reconnect, jobRunning()) : null,
      !aws && !up ? button("Start the control plane", "primary", startDeploy, jobRunning()) : null,
      aws ? button("Remove everything", "danger", startDestroy, jobRunning()) : null),
    aws ? null : el("p", { class: "help" }, "To stop, press Ctrl+C in the terminal. To start again later, run ",
      el("code", {}, "clanker up"), ". To remove the local environment, run ", el("code", {}, "clanker destroy --env local"), "."),
  ];
}

const DRAW = { target: drawTarget, checks: drawChecks, key: drawKey, signin: drawSignIn, where: drawWhere,
  review: drawReview, create: drawCreate, handover: drawHandover };

function draw() {
  drawSteps();
  main.replaceChildren(...DRAW[step]().flat().filter(Boolean));
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
  form.size = view.state.size || form.size;
  if (view.state.deployed) step = "handover";
  if (jobRunning()) {
    step = { deploy: "create", model: "key" }[view.job.name] ?? step;
    watchJob();
  }
  draw();
}

start();
