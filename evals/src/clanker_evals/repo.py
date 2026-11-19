"""Access to the checkout that the evals test: prompts, release notes, stacks and runbooks.

Every file is read at run time, so the evals test the text that production sends.
"""

import json
from functools import cache
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[3]
PROMPTS_DIR = ROOT / "internal" / "agent" / "prompts"
REPLAY_DIR = ROOT / "internal" / "agent" / "replay"
POLICY_DIR = ROOT / "runbooks" / "policy"
PACKS_DIR = ROOT / "apps"
MANIFEST_NAME = "pack.json"


def _find_pack() -> Path:
    """Return the directory of the one application pack, as pack.Discover does in Go."""
    found = sorted(path.parent for path in PACKS_DIR.glob(f"*/{MANIFEST_NAME}"))
    if len(found) != 1:
        raise RuntimeError(f"{PACKS_DIR} must have one application pack, found {len(found)}")
    return found[0]


# The application pack that the evals test. Every name of the application is from its
# manifest, as in the Go runtime (internal/pack).
PACK_DIR = _find_pack()
PACK: dict[str, Any] = json.loads((PACK_DIR / MANIFEST_NAME).read_text(encoding="utf-8"))
RELEASES_DIR = PACK_DIR / PACK["releases"]["directory"]

PLATFORM_REPO = "platform-live"
RELEASES_REPO: str = PACK["releases"]["repository"]
RUNBOOKS_REPO = "runbooks"

_SKIPPED_DIRS = {".terraform", "node_modules", ".git"}

Repos = dict[str, dict[str, str]]


@cache
def prompt(name: str) -> str:
    """Return the system prompt of a role, exactly as the Go runtime embeds it.

    Args:
        name: One of author, explainer, investigator, ask, reviewer.

    Returns:
        The text of internal/agent/prompts/<name>.txt, with the names of the application
        pack in the place of the placeholders.
    """
    return render((PROMPTS_DIR / f"{name}.txt").read_text(encoding="utf-8"))


def one_of(names: list[str]) -> str:
    """Write a list as "a, b or c". Mirrors oneOf in internal/pack/prompt.go."""
    *first, last = names
    if not first:
        return last
    return ", ".join(first) + " or " + last


def render(text: str) -> str:
    """Put the names of the pack into a prompt. Mirrors Pack.Render in internal/pack."""
    values = {
        "{{releases_repository}}": RELEASES_REPO,
        "{{releases_directory}}": PACK["releases"]["directory"],
        "{{deployments}}": one_of(PACK["deployments"]),
    }
    for placeholder, value in values.items():
        text = text.replace(placeholder, value)
    return text


def release_note(version: str) -> str:
    """Return the release note of a version of the application."""
    return (RELEASES_DIR / f"{version}.md").read_text(encoding="utf-8")


def _read_tree(source: Path, dest: str, into: dict[str, str]) -> None:
    for path in sorted(source.rglob("*")):
        if not path.is_file() or _SKIPPED_DIRS & set(path.parts):
            continue
        try:
            text = path.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            continue
        relative = path.relative_to(source).as_posix()
        into[f"{dest}/{relative}" if dest else relative] = text


@cache
def _seed() -> Repos:
    platform: dict[str, str] = {}
    _read_tree(ROOT / "platform", "platform", platform)
    chart = PACK_DIR / PACK["chart"]
    _read_tree(chart, f"{PACK['mount']}/{PACK['chart']}", platform)
    runbooks: dict[str, str] = {}
    _read_tree(ROOT / "runbooks", "", runbooks)
    releases: dict[str, str] = {}
    _read_tree(RELEASES_DIR, PACK["releases"]["directory"], releases)
    _read_tree(chart, PACK["chart"], releases)
    return {PLATFORM_REPO: platform, RUNBOOKS_REPO: runbooks, RELEASES_REPO: releases}


def platform_file(path: str) -> str:
    """Return a file of the platform-live repository as the checkout has it."""
    return _seed()[PLATFORM_REPO][path]


def load_repos(overlay: dict[str, str | None] | None = None) -> Repos:
    """Build the in-memory copy of the three forge repositories on branch main.

    The layout follows seedForge in cmd/clanker/up.go: platform-live holds platform/ and
    the chart of the application pack at its mount path, runbooks holds the content of
    runbooks/, and the releases repository of the pack holds the release notes and the
    chart.

    Args:
        overlay: Changes for one sample, keyed "repo:path". A string replaces or adds the
            file. None removes it.

    Returns:
        A new mapping repo -> path -> content that the caller may change.
    """
    repos = {repo: dict(files) for repo, files in _seed().items()}
    for key, content in (overlay or {}).items():
        repo, _, path = key.partition(":")
        if repo not in repos:
            raise ValueError(f"overlay key {key!r} names no repository; use 'repo:path'")
        if content is None:
            repos[repo].pop(path, None)
        else:
            repos[repo][path] = content
    return repos
