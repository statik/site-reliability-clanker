"""Text helpers that behave like the Go runtime: JSON encoding, truncation and scrubbing."""

import json
import re
from collections.abc import Iterable
from typing import Any

# The rules of internal/scrub/scrub.go: the Go pattern and the Go replacement, unchanged.
# tests/test_mirrors.py fails when this list and the Go file differ.
GO_SCRUB_RULES: list[tuple[str, str]] = [
    (r"(?i)([a-z][a-z0-9+.-]*://[^:/\s@]*):[^@\s]+@", "$1:[REDACTED]@"),
    (r"eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}", "[REDACTED-TOKEN]"),
    (r"\b(AKIA|ASIA)[A-Z0-9]{16}\b", "[REDACTED-AWS-KEY]"),
    (r"\bsk-[A-Za-z0-9_-]{16,}\b", "[REDACTED-API-KEY]"),
    (r"\b(ghp|gho|ghu|ghs|ghr|github_pat)_[A-Za-z0-9_]{20,}", "[REDACTED-TOKEN]"),
    (r"(?i)((?:bearer|basic)\s+)[A-Za-z0-9._~+/=-]{8,}", "$1[REDACTED]"),
    (r"""(?i)https?://[^\s"']*(webhook|hooks)[^\s"']*""", "[REDACTED-WEBHOOK]"),
    (
        r"""(?i)([a-z0-9_.-]*(?:password|passwd|_pass|pwd|secret|token|api[_-]?key|_key)"""
        r"""[a-z0-9_.-]*(?:\\*")?\s*[=:]\s*(?:\\*")?)(?:\\[^"\s]|[^\s",}\\])+""",
        "$1[REDACTED]",
    ),
    (
        r"""(?i)(--[a-z0-9-]*(?:password|passwd|token|secret|api-?key) +"?)"""
        r"""[^\s",}-][^\s",}]*""",
        "$1[REDACTED]",
    ),
    (r"""(\bredis-cli\b[^\n|;&\\]*?\s(?:-a|--pass)[ =]"?)[^\s",}]+""", "$1[REDACTED]"),
    (
        r"""(\b(?:sshpass|mysql|mysqldump|mysqladmin|mariadb|mongosh|mongodump)\b"""
        r"""[^\n|;&\\]*?\s-p ?"?)[^\s",}]+""",
        "$1[REDACTED]",
    ),
    (
        r"""(?i)((?:password|passwd|passphrase|credential|secret|token|api[ _-]?key)"""
        r"""s?\s+(?:is|was)\s+(?:now\s+)?"?)[^\s",}]*(?:[a-z][^\s",}]*[0-9]|[0-9][^\s",}]*[a-z])"""
        r"""[^\s",}]*""",
        "$1[REDACTED]",
    ),
    (
        r"-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----",
        "[REDACTED-PRIVATE-KEY]",
    ),
]

# MinKnownLength and knownReplacement of scrub.go.
MIN_KNOWN_LENGTH = 8
KNOWN_REPLACEMENT = "[REDACTED]"

_GO_GROUP = re.compile(r"\$(\d)")
# re.ASCII gives \b, \s and \w the RE2 meaning.
_SCRUB_RULES = [
    (re.compile(pattern, re.ASCII), _GO_GROUP.sub(r"\\g<\1>", replacement))
    for pattern, replacement in GO_SCRUB_RULES
]


def _known_forms(known: Iterable[str]) -> set[str]:
    """Return each known value and its JSON form, as Add stores them in scrub.go."""
    forms: set[str] = set()
    for value in known:
        if len(value.encode("utf-8")) < MIN_KNOWN_LENGTH:
            continue
        forms.add(value)
        forms.add(go_json(value)[1:-1])
    return forms


def _replace_known(text: str, known: Iterable[str]) -> str:
    """Replace every occurrence of a known value, as replaceKnown does in scrub.go."""
    spans: list[tuple[int, int]] = []
    for value in _known_forms(known):
        at = text.find(value)
        while at >= 0:
            spans.append((at, at + len(value)))
            at = text.find(value, at + 1)
    parts: list[str] = []
    end = 0
    for start, stop in sorted(spans):
        if start >= end:
            parts += [text[end:start], KNOWN_REPLACEMENT]
        end = max(end, stop)
    return "".join([*parts, text[end:]])


def scrub(text: str, known: Iterable[str] = ()) -> str:
    """Return text with every known value and secret pattern replaced, as scrub.Text does.

    Args:
        text: The text that leaves the control plane.
        known: The secret values that the control plane registered with scrub.Add.
    """
    text = _replace_known(text, known)
    for pattern, replacement in _SCRUB_RULES:
        text = pattern.sub(replacement, text)
    return text


def truncate(text: str, limit: int) -> str:
    """Cut text to limit bytes and add "...", as truncate does in internal/agent/llm.go."""
    raw = text.encode("utf-8")
    if len(raw) <= limit:
        return text
    return raw[:limit].decode("utf-8", errors="ignore") + "..."


def go_json(value: Any) -> str:
    """Encode a value as encoding/json does: compact, with <, > and & escaped.

    Go sorts the keys of a map and keeps the field order of a struct. The caller gives
    dictionaries in the order that Go would write; use sorted_map for a Go map.
    """
    encoded = json.dumps(value, separators=(",", ":"), ensure_ascii=False)
    return encoded.replace("<", "\\u003c").replace(">", "\\u003e").replace("&", "\\u0026")


def sorted_map(value: dict[str, Any]) -> dict[str, Any]:
    """Return the dictionary with its keys in the order that Go writes a map."""
    return dict(sorted(value.items()))
