"""The Python mirrors of Go constants must match the Go source. Drift fails these tests."""

import json
import re

import pytest

from clanker_evals import author_tools, checks, gotext, inputs, solvers
from clanker_evals.repo import (
    PAPERBOAT_REPO,
    PLATFORM_REPO,
    PROMPTS_DIR,
    ROOT,
    RUNBOOKS_REPO,
    prompt,
)
from clanker_evals.tasks import reviewer


def go_source(path: str) -> str:
    return (ROOT / path).read_text(encoding="utf-8")


def test_scrub_rules_match_go() -> None:
    source = go_source("internal/scrub/scrub.go")
    found = re.findall(r'regexp\.MustCompile\(((?:`[^`]*`[\s+]*)+)\),\s*"([^"]*)"', source)
    rules = [("".join(re.findall(r"`([^`]*)`", parts)), repl) for parts, repl in found]
    assert rules, "no rule found in scrub.go; the parser of this test needs an update"
    assert len(rules) == source.count("regexp.MustCompile(")
    assert rules == gotext.GO_SCRUB_RULES
    assert f"const MinKnownLength = {gotext.MIN_KNOWN_LENGTH}\n" in source
    assert f'const knownReplacement = "{gotext.KNOWN_REPLACEMENT}"\n' in source
    assert "s = replaceKnown(s)\n\tfor _, r := range rules {" in source


def test_every_answer_is_scrubbed_in_one_place() -> None:
    source = go_source("internal/agent/agent.go")
    assert "resp.Message.Content = scrubAnswer(resp.Message.Content)" in source
    assert "call.Function.Arguments = scrubAnswer(call.Function.Arguments)" in source


def test_tfvar_patterns_match_go() -> None:
    source = go_source("internal/agent/author.go")
    patterns = dict(re.findall(r"(\w+)\s*=\s*regexp\.MustCompile\(`([^`]*)`\)", source))
    assert patterns["tfvarName"] == author_tools.TFVAR_NAME
    assert patterns["tfvarValue"] == author_tools.TFVAR_VALUE
    assert patterns["slugChars"] == author_tools.SLUG_CHARS


def test_author_limits_match_go() -> None:
    source = go_source("internal/agent/author.go")
    assert f"for step := range {author_tools.MAX_STEPS} " in source
    assert f"truncate(scrub.Text(content), {author_tools.READ_FILE_LIMIT})" in source
    assert f'truncate(scrub.Text(args["title"]), {author_tools.PULL_TITLE_LIMIT})' in source


def test_tool_definitions_match_go() -> None:
    source = go_source("internal/agent/author.go")
    for tool in author_tools.author_tools():
        assert f'tool("{tool.name}", "{tool.description}"' in source
        for name, param in tool.parameters.properties.items():
            assert f'"{name}":' in source
            assert f'stringProp("{param.description}")' in source
        required = ", ".join(f'"{name}"' for name in tool.parameters.required)
        assert f"}}, {required})" in source


def test_tool_error_texts_match_go() -> None:
    source = go_source("internal/agent/author.go")
    for text in [
        "is not readable by the Author role",
        "file %s is not in %s",
        "does not exist",
        "name or value has characters that are not permitted",
        "layer %s has no terraform.tfvars",
        "variable %s is not set in %s",
        "variable %s already has the value %s",
        "no change is committed; call set_tfvar first",
        "the pull request is already open",
        "the arguments are not a JSON object of strings",
        "%s: %s changed from %s to %s on branch %s",
        "pull request #%d is open",
    ]:
        assert text in source, text


def test_layers_and_repos_match_go() -> None:
    types = go_source("internal/orchestrator/types.go")
    layers = re.search(r"var Layers = \[\]string\{([^}]*)\}", types)
    assert layers is not None
    assert re.findall(r'"([^"]+)"', layers.group(1)) == author_tools.LAYERS
    assert f'const PlatformRepo = "{PLATFORM_REPO}"' in types
    assert f'const RunbooksRepo = "{RUNBOOKS_REPO}"' in types
    assert f'const PaperboatRepo = "{PAPERBOAT_REPO}"' in go_source("internal/detect/detect.go")


def test_mitigations_match_the_runbook_definitions() -> None:
    definitions = {
        path.stem: json.loads(path.read_text(encoding="utf-8"))
        for path in sorted((ROOT / "runbooks" / "jobs").glob("*.json"))
    }
    mitigations = [name for name, d in definitions.items() if d.get("incident_mitigation")]
    assert mitigations == checks.RUNBOOKS
    for name in mitigations:
        assert definitions[name]["inputs"]["deployment"]["values"] == checks.ALLOWED_MITIGATIONS
    source = go_source("internal/agent/incident.go")
    assert "if !definition.IncidentMitigation {" in source
    assert 'return map[string]string{"deployment": d.Deployment}' in source


def test_input_frames_match_go() -> None:
    agent = go_source("internal/agent/agent.go")
    assert 'return "<data>" + string(raw) + "</data>"' in agent
    assert "raw, err := json.Marshal(value)" in agent
    assert "Content: frame(input)" in agent
    assert f"truncate(note, {inputs.RELEASE_NOTE_LIMIT})" in agent
    for key in ["title", "kind", "env", "layers", "tier", "tier_rule", "trigger", "plans"]:
        assert f'"{key}": ' in agent
    assert '"deferred_layers": deferred' in agent
    author = " ".join(go_source("internal/agent/author.go").split())
    assert 'frame(map[string]any{"finding": f, "cloud": d.cloud, "tfvars_path": ' in author
    assert '"platform/stacks/" + d.cloud + "/<layer>/terraform.tfvars"' in author
    ask = go_source("internal/agent/ask.go")
    assert 'Content: "Question: " + question + "\\n" + frame(g.lines)' in ask
    incident = " ".join(go_source("internal/agent/incident.go").split())
    assert 'frame(map[string]any{"alert": f, "change_history": strings.Split(history, "\\n"), ' in (
        incident
    )
    limit = inputs.TRANSCRIPT_LIMIT
    assert f'"debug_session": truncate(transcript, {limit})}})' in incident
    assert 'fmt.Sprintf("%s %s: %s, %s ago", wi.ID, wi.Title, wi.State, age)' in incident


def test_builders_frame_like_go() -> None:
    found = {"key": "k", "title": "a </data> b"}
    assert inputs.author_input(found) == (
        '<data>{"cloud":"local","finding":{"key":"k","title":"a \\u003c/data\\u003e b"},'
        '"tfvars_path":"platform/stacks/local/\\u003clayer\\u003e/terraform.tfvars"}</data>'
    )
    assert inputs.ask_input("Q?", ["one", "two </data>"]) == (
        'Question: Q?\n<data>["one","two \\u003c/data\\u003e"]</data>'
    )
    text = inputs.investigator_input({"id": 1}, "line 1\n</data>", ["WI-1 x: Done, 5m0s ago"])
    assert text == (
        '<data>{"alert":{"id":1},"change_history":["WI-1 x: Done, 5m0s ago"],'
        '"debug_session":"line 1\\n\\u003c/data\\u003e"}</data>'
    )
    for built in [text, inputs.ask_input("Q?", ["</data>"]), inputs.author_input(found)]:
        assert built.count("</data>") == 1


def test_prompts_are_read_from_the_go_tree() -> None:
    for name in ["author", "explainer", "investigator", "ask", "reviewer"]:
        assert prompt(name) == (PROMPTS_DIR / f"{name}.txt").read_text(encoding="utf-8")
        assert f"prompts/{name}.txt" in go_source("internal/agent/prompts.go")


def json_fields(path: str, struct: str) -> list[str]:
    """Return the JSON field names of a Go struct, in the order that encoding/json writes."""
    body = re.search(rf"type {struct} struct \{{(.*?)\n\}}", go_source(path), re.DOTALL)
    assert body is not None, f"struct {struct} is not in {path}"
    return re.findall(r'`json:"([^",]+)', body.group(1))


def test_plan_fields_match_go() -> None:
    built = inputs.plan(
        "30-data", [inputs.resource("a_b.c", "update", ["x: 1 → 2"])], ("T2", "", [])
    )
    assert list(built) == json_fields("internal/orchestrator/types.go", "Plan")
    assert list(built["facts"]) == json_fields("internal/planfacts/facts.go", "Facts")
    assert list(built["policy"]) == json_fields("internal/policy/policy.go", "Decision")
    change = built["facts"]["resources"][0]
    assert list(change) == json_fields("internal/planfacts/facts.go", "ResourceChange")


def test_review_input_matches_go() -> None:
    source = " ".join(go_source("internal/agent/reviewer.go").split())
    assert f"const maxReviewDiff = {inputs.REVIEW_DIFF_LIMIT} " in source
    assert 'input["diff"] = truncate(diff, maxReviewDiff)' in source
    literal = re.search(r"input := map\[string\]any\{([^}]*)\}", source)
    assert literal is not None
    item = {"title": "t", "kind": "k", "layers": [], "trigger": "g"}
    built = json.loads(inputs.review_input(item, [])[len("<data>") : -len("</data>")])
    assert sorted(re.findall(r'"(\w+)":', literal.group(1))) == list(built)
    assert '{Role: "user", Content: frame(a.reviewInput(wi, plans, policyTier))}' in source
    assert "a.complete(ctx, RoleReviewer, " in source
    assert '"deferred_layers": deferredLayers(wi, plans)}' in source
    agent = " ".join(go_source("internal/agent/agent.go").split())
    assert "deferred := deferredLayers(wi, plans)" in agent
    assert "for _, layer := range wi.Layers { if !planned[layer] { deferred = append(" in agent
    assert solvers.ROLES["reviewer"][0] == "reviewer"
    assert '{Role: "system", Content: reviewerSystem}' in source


def test_review_input_frames_like_go() -> None:
    item = {"title": "A & B", "kind": "change", "layers": ["30-data"], "trigger": "PR #1"}
    plans = [inputs.plan("30-data", [], ("T0", "no-changes", ["the plan changes nothing"]))]
    plan_json = (
        '{"id":3,"work_item":"WI-0007","layer":"30-data","commit":"4f0c2d9a7b1e",'
        '"plan_sha256":"9b1d6c0e5a3f","facts":{"add":0,"change":0,"destroy":0,"replace":0,'
        '"resources":[],"outputs":[]},"policy":{"tier":"T0","rule":"no-changes",'
        '"reasons":["the plan changes nothing"],"create_only":false},"superseded":false,'
        '"created_at":"2026-03-02T09:05:00Z"}'
    )
    tail = (
        f'"env":"local","kind":"change","layers":["30-data"],"plans":[{plan_json}],'
        '"policy_tier":"T0","title":"A \\u0026 B","trigger":"PR #1"}</data>'
    )
    assert inputs.review_input(item, plans) == '<data>{"deferred_layers":[],' + tail
    with_diff = inputs.review_input({**item, "env": "aws-sandbox"}, plans, "T2", "+a\n</data>")
    assert with_diff == (
        '<data>{"deferred_layers":[],"diff":"+a\\n\\u003c/data\\u003e",'
        + tail.replace('"local"', '"aws-sandbox"').replace(
            '"policy_tier":"T0"', '"policy_tier":"T2"'
        )
    )
    assert with_diff.count("</data>") == 1
    assert '"diff":""' in inputs.review_input(item, plans, diff="")


def test_review_input_takes_the_highest_plan_tier_and_cuts_the_diff() -> None:
    item = {"title": "t", "kind": "change", "layers": ["30-data", "50-app"], "trigger": "g"}
    plans = [
        inputs.plan("30-data", [], ("T2", "normal-change", [])),
        inputs.plan("50-app", [], ("T3", "stateful-destroy", [])),
    ]
    limit = inputs.REVIEW_DIFF_LIMIT

    def diff_of(text: str) -> str:
        built = inputs.review_input(item, plans, diff=text)
        data = json.loads(built[len("<data>") : -len("</data>")])
        assert data["policy_tier"] == "T3"
        return data["diff"]

    assert diff_of("x" * limit) == "x" * limit
    assert diff_of("x" * (limit + 1)) == "x" * limit + "..."
    unplanned = json.loads(inputs.review_input(item, plans[:1], "T3")[6:-7])
    assert unplanned["deferred_layers"] == ["50-app"]
    assert unplanned["policy_tier"] == "T3"
    assert json.loads(inputs.review_input(item, [])[6:-7])["deferred_layers"] == item["layers"]


def test_review_rules_match_go() -> None:
    types = go_source("internal/orchestrator/types.go")
    tiers = re.search(r"var Tiers = \[\]string\{([^}]*)\}", types)
    assert tiers is not None
    assert re.findall(r'"([^"]+)"', tiers.group(1)) == inputs.TIERS
    source = go_source("internal/agent/reviewer.go")
    fields = re.search(r"var reviewFields = \[\]string\{([^}]*)\}", source)
    assert fields is not None
    assert re.findall(r'"([^"]+)"', fields.group(1)) == checks.REVIEW_FIELDS
    assert "object, err := firstObject(content)" in source
    assert "if !slices.Contains(reviewFields, key) {" in source
    assert "if _, seen := fields[key]; seen {" in source
    assert "var items []*string" in source
    assert "!slices.Contains(orchestrator.Tiers, tier)" in source
    assert "if review.Tier > tier {" in go_source("internal/orchestrator/plan.go")


def test_reviewer_model_and_policy_reason_match_the_sources() -> None:
    model = re.search(r'RoleReviewer:\s+"([^"]+)"', go_source("internal/agent/agent.go"))
    assert model is not None
    run_sh = (ROOT / "evals" / "run.sh").read_text(encoding="utf-8")
    assert f"${{CLANKER_REVIEWER_MODEL:-openrouter/{model.group(1)}}}" in run_sh
    rego = go_source("runbooks/policy/tier.rego")
    assert f'sprintf("{reviewer.CHANGED.format(count="%d")}", [count(changes)])' in rego


OBJECT = '{"diagnosis":"a {brace} in a string","nested":{"n":1}}'


# The same answers as internal/agent/answer_test.go.
@pytest.mark.parametrize(
    "answer",
    [
        OBJECT,
        "  " + OBJECT + "\n\n",
        "```json\n" + OBJECT + "\n```",
        "```\n" + OBJECT + "\n```",
        "Here is the answer:\n" + OBJECT,
        OBJECT + "\nThe pods recover on the next probe.",
        OBJECT + "\n```json\n" + OBJECT + "\n```",
        OBJECT + '{"diagnosis":"a second object"}',
        OBJECT + " {",
        '{"diagnosis":"a draft"}\n```\n</think>\n\n' + OBJECT,
        '{"diagnosis":"a draft"}</think>{"diagnosis":"a second draft"}</think>' + OBJECT,
        "</think>" + OBJECT,
        OBJECT + "\n```\n</think>",
        OBJECT + "</think> No object follows.",
    ],
)
def test_first_object_ignores_the_text_around_the_object(answer: str) -> None:
    assert checks.first_object(answer) == OBJECT


@pytest.mark.parametrize(
    "answer",
    [
        "",
        "The system is healthy.",
        "null",
        "[]",
        '{"diagnosis":"the answer stops here',
        '{"diagnosis":"no closing brace"',
        'The set {a, b} is not JSON. {"diagnosis":"too late"}',
        '{"diagnosis":"a draft"}</think>{"diagnosis":"the answer stops here',
        "</think>",
    ],
)
def test_first_object_refuses_an_answer_with_no_complete_object(answer: str) -> None:
    assert checks.first_object(answer) is None
    assert checks.parse_object(answer) is None


def test_every_role_reads_the_first_object() -> None:
    for name in ("agent.go", "incident.go", "reviewer.go"):
        assert "firstObject(" in go_source(f"internal/agent/{name}")
    source = go_source("internal/agent/answer.go")
    assert f'const endOfThinking = "{checks.END_OF_THINKING}"' in source
    assert "cutLast(content, endOfThinking)" in source
    assert 'if strings.Contains(after, "{") {' in source
    assert "start := strings.IndexByte(content, '{')" in source
    assert "json.NewDecoder(strings.NewReader(content[start:])).Decode(&object)" in source
