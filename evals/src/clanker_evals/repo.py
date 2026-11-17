"""Access to the checkout that the evals test: prompts, release notes, stacks and runbooks.

Every file is read at run time, so the evals test the text that production sends.
"""

from functools import cache
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
PROMPTS_DIR = ROOT / "internal" / "agent" / "prompts"
REPLAY_DIR = ROOT / "internal" / "agent" / "replay"
RELEASES_DIR = ROOT / "apps" / "paperboat" / "releases"
POLICY_DIR = ROOT / "runbooks" / "policy"

PLATFORM_REPO = "platform-live"
PAPERBOAT_REPO = "paperboat"
RUNBOOKS_REPO = "runbooks"

_SKIPPED_DIRS = {".terraform", "node_modules", ".git"}

Repos = dict[str, dict[str, str]]


@cache
def prompt(name: str) -> str:
    """Return the system prompt of a role, exactly as the Go runtime embeds it.

    Args:
        name: One of author, explainer, investigator, ask, reviewer.

    Returns:
        The text of internal/agent/prompts/<name>.txt.
    """
    return (PROMPTS_DIR / f"{name}.txt").read_text(encoding="utf-8")


def release_note(version: str) -> str:
    """Return the release note of a Paperboat version."""
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
    _read_tree(ROOT / "apps" / "paperboat" / "chart", "apps/paperboat/chart", platform)
    runbooks: dict[str, str] = {}
    _read_tree(ROOT / "runbooks", "", runbooks)
    paperboat: dict[str, str] = {}
    _read_tree(RELEASES_DIR, "releases", paperboat)
    _read_tree(ROOT / "apps" / "paperboat" / "chart", "chart", paperboat)
    return {PLATFORM_REPO: platform, RUNBOOKS_REPO: runbooks, PAPERBOAT_REPO: paperboat}


def platform_file(path: str) -> str:
    """Return a file of the platform-live repository as the checkout has it."""
    return _seed()[PLATFORM_REPO][path]


def load_repos(overlay: dict[str, str | None] | None = None) -> Repos:
    """Build the in-memory copy of the three forge repositories on branch main.

    The layout follows seedForge in cmd/clanker/up.go: platform-live holds platform/ and
    apps/paperboat/chart/, runbooks holds the content of runbooks/, and paperboat holds
    releases/ and chart/.

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
