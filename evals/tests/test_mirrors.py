"""The Python mirrors of Go constants must match the Go source. Drift fails these tests."""

import re

from clanker_evals import author_tools, checks, gotext, inputs
from clanker_evals.repo import (
    PAPERBOAT_REPO,
    PLATFORM_REPO,
    PROMPTS_DIR,
    ROOT,
    RUNBOOKS_REPO,
    prompt,
)


def go_source(path: str) -> str:
    return (ROOT / path).read_text(encoding="utf-8")


def test_scrub_rules_match_go() -> None:
    source = go_source("internal/scrub/scrub.go")
    found = re.findall(r'regexp\.MustCompile\(((?:`[^`]*`[\s+]*)+)\),\s*"([^"]*)"', source)
    rules = [("".join(re.findall(r"`([^`]*)`", parts)), repl) for parts, repl in found]
    assert rules, "no rule found in scrub.go; the parser of this test needs an update"
    assert len(rules) == source.count("regexp.MustCompile(")
    assert rules == gotext.GO_SCRUB_RULES


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


def test_mitigations_match_go() -> None:
    source = go_source("internal/agent/incident.go")
    allowed = re.search(r"allowedMitigations = map\[string\]bool\{([^}]*)\}", source)
    assert allowed is not None
    assert re.findall(r'"([^"]+)": true', allowed.group(1)) == checks.ALLOWED_MITIGATIONS
    for runbook in checks.RUNBOOKS:
        assert f'd.ProposedRunbook != "{runbook}"' in source


def test_input_frames_match_go() -> None:
    agent = go_source("internal/agent/agent.go")
    assert 'return "<data>" + string(raw) + "</data>"' in agent
    assert "raw, err := json.Marshal(value)" in agent
    assert "Content: frame(input)" in agent
    assert "truncate(note, 4000)" in agent
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
    assert '"debug_session": truncate(transcript, 8000)})' in incident
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
    for name in ["author", "explainer", "investigator", "ask"]:
        assert prompt(name) == (PROMPTS_DIR / f"{name}.txt").read_text(encoding="utf-8")
        assert f"prompts/{name}.txt" in go_source("internal/agent/prompts.go")
