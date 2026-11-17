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

const REGIONS = ["us-east-1", "us-east-2", "us-west-2", "ca-central-1", "eu-west-1", "eu-west-2",
  "eu-central-1", "eu-north-1", "ap-southeast-1", "ap-southeast-2", "ap-northeast-1", "ap-south-1",
  "sa-east-1"];

const STEPS = [
  { id: "target", title: "Where to run" },
  { id: "checks", title: "Check this machine" },
  { id: "key", title: "Model key" },
  { id: "signin", title: "Sign in", only: "aws" },
  { id: "review", title: "Review" },
  { id: "create", title: "Create" },
  { id: "handover", title: "Hand over" },
];

let view = null;
let step = "target";
let message = null;
let timer = null;
const form = { target: "", env: "clanker", key: "", profile: "", region: "us-east-1",
  size: "small", profiles: null, checks: null };

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
    job.lines.length > 0 ? el("div", { class: "log", tabindex: "0" }, job.lines.join("\n")) : null,
    job.status === "failed" ? el("div", { class: "notice notice-error", role: "alert" },
      el("strong", {}, "This step failed."), job.error) : null);
}

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
      choice("aws", "Your AWS account", view.aws_enabled
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

// ---------------------------------------------------------------- 3. model key

function drawKey() {
  const where = targetOf() === "aws"
    ? "The wizard stores it encrypted in AWS Systems Manager Parameter Store in your account. " +
      "Only the control plane can read it. It is not written to a file here."
    : "The wizard saves it in the data directory on this machine, readable only by you.";
  return [
    el("h1", {}, "Give S.R.C. a language model."),
    el("p", { class: "lead" }, "The model drafts changes and writes the summaries. It gets no cloud " +
      "credential. S.R.C. uses OpenRouter, and stops at 5 USD each day."),
    el("label", { class: "field" }, "OpenRouter API key",
      el("input", { type: "password", autocomplete: "off", spellcheck: "false", placeholder: "sk-or-…",
        value: form.key, oninput: (event) => { form.key = event.target.value; } }),
      el("span", { class: "help" }, where)),
    notice(),
    buttons(
      button("Back", "secondary", () => go("checks")),
      button("Continue without a key", "secondary", () => saveKey("")),
      button("Save the key and continue", "primary", () => saveKey(form.key))),
    el("p", { class: "help" }, "Without a key, S.R.C. uses recorded answers. They fit the example " +
      "application only."),
  ];
}

async function saveKey(key) {
  if (key === "" && form.key !== "" && !window.confirm("Continue without the key that you typed?")) return;
  try {
    await call("POST", "/api/key", { key });
    form.key = "";
    await refresh();
    go(targetOf() === "aws" ? "signin" : "review");
  } catch (error) { fail(error); }
}

// ---------------------------------------------------------------- 4. sign in

async function loadProfiles() {
  try {
    form.profiles = (await call("GET", "/api/aws/profiles")).profiles;
    form.profile = view.state.profile || form.profiles[0] || "";
    form.region = view.state.region || form.region;
  } catch (error) { form.profiles = []; fail(error); return; }
  draw();
}

async function signIn() {
  const body = { profile: form.profile, region: form.region, size: form.size };
  try {
    await call("POST", "/api/aws/signin", body);
    await refresh();
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
  const signedIn = Boolean(state.account) && state.profile === form.profile && state.region === form.region;
  return [
    el("h1", {}, "Sign in to your AWS account."),
    el("p", { class: "lead" }, "Choose a profile of the AWS command line. The wizard works as you, " +
      "with your permissions, and only while it runs. You need permission to create a network, " +
      "IAM roles, a file system and an ECS service."),
    form.profiles.length === 0 ? el("div", { class: "notice notice-warning" },
      "The AWS command line has no profile on this machine. Run ", el("code", {}, "aws configure sso"),
      " in a terminal, then load this page again.") : null,
    el("label", { class: "field" }, "Profile",
      el("select", { onchange: (event) => { form.profile = event.target.value; draw(); } },
        form.profiles.map((name) => el("option", { value: name, selected: name === form.profile }, name)))),
    el("label", { class: "field" }, "Region",
      el("select", { onchange: (event) => { form.region = event.target.value; draw(); } },
        REGIONS.map((name) => el("option", { value: name, selected: name === form.region }, name)))),
    el("label", { class: "field" }, "Size of the container",
      el("select", { onchange: (event) => { form.size = event.target.value; } },
        [["small", "Small: 0.5 vCPU and 1 GB of memory, about 14 USD each month"],
          ["medium", "Medium: 1 vCPU and 2 GB of memory, about 29 USD each month"]].map(([value, label]) =>
          el("option", { value, selected: value === form.size }, label)))),
    signedIn ? el("dl", { class: "facts" },
      el("div", {}, el("dt", {}, "Account"), el("dd", {}, state.account)),
      el("div", {}, el("dt", {}, "Signed in as"), el("dd", {}, state.principal.split("/").slice(-1)[0])),
      el("div", {}, el("dt", {}, "Region"), el("dd", {}, state.region))) : null,
    view.job?.name === "login" ? jobBlock() : null,
    notice(),
    buttons(
      button("Back", "secondary", () => go("key")),
      button(signedIn ? "Sign in again" : "Sign in", signedIn ? "secondary" : "primary", signIn,
        jobRunning() || form.profile === ""),
      button("Continue", "primary", () => go("review"), !signedIn || jobRunning())),
  ];
}

// ---------------------------------------------------------------- 5. review

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
      button("Back", "secondary", () => go(aws ? "signin" : "key")),
      reviewJob?.status === "failed" ? button("Try the review again", "secondary", startReview) : null,
      button(aws ? "Create these resources" : "Start the control plane", "primary", startDeploy,
        !reviewed || jobRunning())),
  ];
}

// ---------------------------------------------------------------- 6. create

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

// ---------------------------------------------------------------- 7. hand over

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

const DRAW = { target: drawTarget, checks: drawChecks, key: drawKey, signin: drawSignIn,
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
    step = view.job.name === "deploy" ? "create" : step;
    watchJob();
  }
  draw();
}

start();
