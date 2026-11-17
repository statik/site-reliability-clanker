"""Text helpers that behave like the Go runtime: JSON encoding, truncation and scrubbing."""

import json
import re
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
        r"""[a-z0-9_.-]*"?\s*[=:]\s*"?)[^\s",}]+""",
        "$1[REDACTED]",
    ),
    (
        r"-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----",
        "[REDACTED-PRIVATE-KEY]",
    ),
]

_GO_GROUP = re.compile(r"\$(\d)")
# re.ASCII gives \b, \s and \w the RE2 meaning.
_SCRUB_RULES = [
    (re.compile(pattern, re.ASCII), _GO_GROUP.sub(r"\\g<\1>", replacement))
    for pattern, replacement in GO_SCRUB_RULES
]


def scrub(text: str) -> str:
    """Return text with every secret pattern replaced, as scrub.Text does in Go."""
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
