"""The three typed tools of the Author role, with the rules of internal/agent/author.go.

AuthorSession holds the in-memory repositories and the draft state of one Author loop. It
answers a tool call with the same text that the Go runtime gives, and it records every call
so that a scorer can read the trace.
"""

import re
from dataclasses import dataclass, field
from typing import Any

from inspect_ai.tool import ToolInfo, ToolParam, ToolParams

from clanker_evals.gotext import scrub, truncate
from clanker_evals.repo import PAPERBOAT_REPO, PLATFORM_REPO, RUNBOOKS_REPO, Repos

# Mirrors of constants in the Go code. tests/test_mirrors.py compares them with the source.
LAYERS = ["00-bootstrap", "10-network", "20-cluster", "30-data", "40-platform", "50-app"]
READABLE_REPOS = [PLATFORM_REPO, PAPERBOAT_REPO, RUNBOOKS_REPO]
TFVAR_NAME = r"^[a-z][a-z0-9_]{1,60}$"
TFVAR_VALUE = r"^[A-Za-z0-9._-]{1,60}$"
SLUG_CHARS = r"[^a-z0-9]+"
READ_FILE_LIMIT = 6000
PULL_TITLE_LIMIT = 120
MAX_STEPS = 8

# Go anchors ^ and $ at the ends of the text. fullmatch on the inner pattern does the same.
_NAME = re.compile(TFVAR_NAME.removeprefix("^").removesuffix("$"), re.ASCII)
_VALUE = re.compile(TFVAR_VALUE.removeprefix("^").removesuffix("$"), re.ASCII)


def _tool(name: str, description: str, properties: dict[str, str]) -> ToolInfo:
    params = ToolParams(
        properties={
            key: ToolParam(type="string", description=text) for key, text in properties.items()
        },
        required=list(properties),
    )
    return ToolInfo(name=name, description=description, parameters=params)


def author_tools() -> list[ToolInfo]:
    """Return the tool definitions that authorTools sends to the model."""
    return [
        _tool(
            "read_file",
            "Read a file from a repository on the main branch.",
            {"repo": "platform-live, paperboat or runbooks", "path": "path inside the repository"},
        ),
        _tool(
            "set_tfvar",
            "Set one variable in the terraform.tfvars file of a layer.",
            {
                "layer": "layer, for example 50-app",
                "name": "variable name",
                "value": "new value, without quotes",
            },
        ),
        _tool(
            "open_pull_request",
            "Open the pull request for the change. Call it once, last.",
            {"title": "short title", "body": "what changes, why now, rollback"},
        ),
    ]


class ToolRefusedError(Exception):
    """A tool refused the call. The model receives "error: <message>"."""


@dataclass
class AuthorSession:
    """The state of one Author loop: the repositories, the branch, the edits and the pull."""

    repos: Repos
    finding_key: str
    cloud: str = "local"
    branch_files: dict[str, str] = field(default_factory=dict)
    edits: list[dict[str, str]] = field(default_factory=list)
    pull: dict[str, Any] | None = None
    calls: list[dict[str, Any]] = field(default_factory=list)

    @property
    def branch(self) -> str:
        """The branch name that Draft derives from the key of the finding."""
        return "agent/" + re.sub(SLUG_CHARS, "-", self.finding_key.lower()).strip("-")

    def _read_file(self, args: dict[str, str]) -> str:
        repo, path = args.get("repo", ""), args.get("path", "")
        if repo not in READABLE_REPOS:
            raise ToolRefusedError(f'repository "{repo}" is not readable by the Author role')
        content = self.repos[repo].get(path)
        if content is None:
            raise ToolRefusedError(f"file {path} is not in {repo}")
        return truncate(scrub(content), READ_FILE_LIMIT)

    def _set_tfvar(self, args: dict[str, str]) -> str:
        layer, name, value = args.get("layer", ""), args.get("name", ""), args.get("value", "")
        if layer not in LAYERS:
            raise ToolRefusedError(f'layer "{layer}" does not exist')
        if not _NAME.fullmatch(name) or not _VALUE.fullmatch(value):
            raise ToolRefusedError("name or value has characters that are not permitted")
        path = f"platform/stacks/{self.cloud}/{layer}/terraform.tfvars"
        source = self.branch_files if self.edits else self.repos[PLATFORM_REPO]
        content = source.get(path, self.repos[PLATFORM_REPO].get(path))
        if content is None:
            raise ToolRefusedError(f"layer {layer} has no terraform.tfvars")
        line = re.compile(r"(?m)^(" + re.escape(name) + r"\s*=\s*)(.*)$")
        match = line.search(content)
        if match is None:
            raise ToolRefusedError(f"variable {name} is not set in {path}")
        old = match.group(2)
        new = f'"{value}"' if old.startswith('"') else value
        if old == new:
            raise ToolRefusedError(f"variable {name} already has the value {value}")
        self.branch_files[path] = line.sub(lambda m: m.group(1) + new, content)
        self.edits.append({"layer": layer, "name": name, "old": old, "new": new, "value": value})
        return f"{path}: {name} changed from {old} to {new} on branch {self.branch}"

    def _open_pull(self, args: dict[str, str]) -> str:
        if not self.edits:
            raise ToolRefusedError("no change is committed; call set_tfvar first")
        if self.pull is not None:
            raise ToolRefusedError("the pull request is already open")
        self.pull = {
            "id": 1,
            "title": truncate(scrub(args.get("title", "")), PULL_TITLE_LIMIT),
            "body": scrub(args.get("body", "")),
            "branch": self.branch,
        }
        return f"pull request #{self.pull['id']} is open"

    def run(self, name: str, arguments: Any) -> str:
        """Run one tool call and return the text that the model receives.

        Args:
            name: The tool name that the model asked for.
            arguments: The decoded arguments. Go decodes them into a map of strings, so
                anything that is not a JSON object of strings is an error.

        Returns:
            The result, or "error: ..." when the tool refused the call.
        """
        handlers = {
            "read_file": self._read_file,
            "set_tfvar": self._set_tfvar,
            "open_pull_request": self._open_pull,
        }
        valid = isinstance(arguments, dict) and all(isinstance(v, str) for v in arguments.values())
        try:
            if not valid:
                raise ToolRefusedError("the arguments are not a JSON object of strings")
            if name not in handlers:
                raise ToolRefusedError(f'tool "{name}" does not exist')
            result, ok = handlers[name](arguments), True
        except ToolRefusedError as failure:
            result, ok = f"error: {failure}", False
        self.calls.append({"name": name, "arguments": arguments, "result": result, "ok": ok})
        return result

    def trace(self) -> dict[str, Any]:
        """Return the record of the loop for the scorers and the log."""
        return {
            "calls": self.calls,
            "edits": self.edits,
            "pull": self.pull,
            "branch": self.branch,
            "changed_files": sorted(self.branch_files),
        }
