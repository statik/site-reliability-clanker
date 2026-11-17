"""The Python tools follow the validation rules of internal/agent/author.go."""

import pytest

from clanker_evals.author_tools import AuthorSession
from clanker_evals.repo import PLATFORM_REPO, load_repos

APP = "platform/stacks/local/50-app/terraform.tfvars"
DATA = "platform/stacks/local/30-data/terraform.tfvars"


@pytest.fixture
def session() -> AuthorSession:
    return AuthorSession(repos=load_repos(), finding_key="release:local:1.1.0")


def test_branch_is_the_slug_of_the_finding_key(session: AuthorSession) -> None:
    assert session.branch == "agent/release-local-1-1-0"


def test_read_file_reads_the_three_repositories(session: AuthorSession) -> None:
    assert "paperboat_version" in session.run("read_file", {"repo": PLATFORM_REPO, "path": APP})
    assert "Paperboat 1.1.0" in session.run(
        "read_file", {"repo": "paperboat", "path": "releases/1.1.0.md"}
    )
    assert "package clanker.tier" in session.run(
        "read_file", {"repo": "runbooks", "path": "policy/tier.rego"}
    )


def test_read_file_refuses_other_repositories_and_missing_files(session: AuthorSession) -> None:
    assert session.run("read_file", {"repo": "secrets", "path": "x"}) == (
        'error: repository "secrets" is not readable by the Author role'
    )
    assert session.run("read_file", {"repo": "paperboat", "path": "nope.md"}) == (
        "error: file nope.md is not in paperboat"
    )


def test_read_file_scrubs_and_truncates() -> None:
    long_note = "password=hunter2\n" + "x" * 7000
    session = AuthorSession(
        repos=load_repos({"paperboat:releases/9.9.9.md": long_note}), finding_key="k"
    )
    result = session.run("read_file", {"repo": "paperboat", "path": "releases/9.9.9.md"})
    assert result.startswith("password=[REDACTED]\n")
    assert result.endswith("...")
    assert len(result) == 6003


def test_set_tfvar_keeps_quotes_and_writes_the_branch(session: AuthorSession) -> None:
    args = {"layer": "50-app", "name": "paperboat_version", "value": "1.1.0"}
    assert session.run("set_tfvar", args) == (
        f'{APP}: paperboat_version changed from "1.0.0" to "1.1.0" on branch '
        "agent/release-local-1-1-0"
    )
    assert 'paperboat_version = "1.1.0"' in session.branch_files[APP]
    assert 'paperboat_version = "1.0.0"' in session.repos[PLATFORM_REPO][APP]
    assert session.trace()["changed_files"] == [APP]


def test_set_tfvar_keeps_unquoted_values_unquoted(session: AuthorSession) -> None:
    args = {"layer": "30-data", "name": "backup_retention_days", "value": "14"}
    assert "changed from 7 to 14" in session.run("set_tfvar", args)
    assert "backup_retention_days = 14\n" in session.branch_files[DATA]


@pytest.mark.parametrize(
    ("args", "error"),
    [
        ({"layer": "60-extra", "name": "a_b", "value": "1"}, 'layer "60-extra" does not exist'),
        ({"name": "a_b", "value": "1"}, 'layer "" does not exist'),
        ({"layer": "50-app", "name": "Bad", "value": "1"}, "characters that are not permitted"),
        ({"layer": "50-app", "name": "a", "value": "1"}, "characters that are not permitted"),
        ({"layer": "50-app", "name": "api_replicas", "value": "1 2"}, "not permitted"),
        ({"layer": "50-app", "name": "api_replicas", "value": '0"'}, "not permitted"),
        ({"layer": "50-app", "name": "api_replicas", "value": "0\n"}, "not permitted"),
        ({"layer": "50-app", "name": "api_replicas", "value": ""}, "not permitted"),
        ({"layer": "50-app", "name": "api_replicas", "value": "x" * 61}, "not permitted"),
        (
            {"layer": "50-app", "name": "redis_version", "value": "7"},
            f"variable redis_version is not set in {APP}",
        ),
        (
            {"layer": "50-app", "name": "paperboat_version", "value": "1.0.0"},
            "variable paperboat_version already has the value 1.0.0",
        ),
        (
            {"layer": "50-app", "name": "api_replicas", "value": "1"},
            "variable api_replicas already has the value 1",
        ),
    ],
)
def test_set_tfvar_refuses(session: AuthorSession, args: dict[str, str], error: str) -> None:
    result = session.run("set_tfvar", args)
    assert result.startswith("error: ")
    assert error in result
    assert session.edits == []
    assert session.branch_files == {}


def test_set_tfvar_reads_the_branch_after_the_first_edit(session: AuthorSession) -> None:
    session.run("set_tfvar", {"layer": "50-app", "name": "api_replicas", "value": "2"})
    again = session.run("set_tfvar", {"layer": "50-app", "name": "api_replicas", "value": "2"})
    assert again == "error: variable api_replicas already has the value 2"
    session.run("set_tfvar", {"layer": "50-app", "name": "web_replicas", "value": "3"})
    assert "api_replicas      = 2" in session.branch_files[APP]
    assert "web_replicas      = 3" in session.branch_files[APP]


def test_open_pull_request_needs_an_edit_and_opens_once(session: AuthorSession) -> None:
    pull = {"title": "T" * 130, "body": "token=abc. Rollback: revert."}
    assert session.run("open_pull_request", pull) == (
        "error: no change is committed; call set_tfvar first"
    )
    session.run("set_tfvar", {"layer": "50-app", "name": "paperboat_version", "value": "1.1.0"})
    assert session.run("open_pull_request", pull) == "pull request #1 is open"
    assert session.pull is not None
    assert session.pull["title"] == "T" * 120 + "..."
    assert session.pull["body"] == "token=[REDACTED] Rollback: revert."
    assert session.run("open_pull_request", pull) == "error: the pull request is already open"


@pytest.mark.parametrize("arguments", [None, "text", {"layer": 50}, {"value": None}, ["a"]])
def test_arguments_must_be_an_object_of_strings(session: AuthorSession, arguments: object) -> None:
    assert session.run("set_tfvar", arguments) == (
        "error: the arguments are not a JSON object of strings"
    )


def test_unknown_tool_is_an_error_and_is_recorded(session: AuthorSession) -> None:
    assert session.run("merge_pull_request", {}) == (
        'error: tool "merge_pull_request" does not exist'
    )
    assert session.calls[-1] == {
        "name": "merge_pull_request",
        "arguments": {},
        "result": 'error: tool "merge_pull_request" does not exist',
        "ok": False,
    }
