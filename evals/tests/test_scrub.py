"""Behaviour of the scrub mirror, and the secret forms that scrub.go does not catch."""

import pytest

from clanker_evals.gotext import go_json, scrub, truncate
from clanker_evals.solvers import KNOWN_SECRETS_KEY
from clanker_evals.tasks.injection import DB_CANARY, DB_URL, TOKEN_CANARY, TOKEN_LINE
from clanker_evals.tasks.secrets import (
    CANARIES,
    CAUGHT,
    FIXED,
    KNOWN,
    UNCAUGHT,
    leakage_samples,
)


@pytest.mark.parametrize(
    ("text", "expected"),
    [
        ("postgres://app:hunter2@db:5432/x", "postgres://app:[REDACTED]@db:5432/x"),
        ("HTTPS://user:pw@example.com/", "HTTPS://user:[REDACTED]@example.com/"),
        ("jwt eyJhbGciOiJIUzI1.eyJzdWIiOiIxMjM0.SflKxwRJSMeKKF2Q", "jwt [REDACTED-TOKEN]"),
        ("key AKIAABCDEFGHIJKLMNOP end", "key [REDACTED-AWS-KEY] end"),
        ("sk-abcdefghijklmnop1234", "[REDACTED-API-KEY]"),
        ("https://hooks.slack.com/services/T0/B0/abc", "[REDACTED-WEBHOOK]"),
        ("https://discord.com/api/webhooks/1/a_b-c", "[REDACTED-WEBHOOK]"),
        ("https://discordapp.com/api/webhooks/1/abc", "[REDACTED-WEBHOOK]"),
        ("password=hunter2", "password=[REDACTED]"),
        ('Password: "hunter2"', 'Password: "[REDACTED]"'),
        ("api_key = abc, next", "api_key = [REDACTED], next"),
        ("api-key:abc}", "api-key:[REDACTED]}"),
        ("secret=a token=b", "secret=[REDACTED] token=[REDACTED]"),
        (
            "-----BEGIN RSA PRIVATE KEY-----\nabc\n-----END RSA PRIVATE KEY-----",
            "[REDACTED-PRIVATE-KEY]",
        ),
        ("nothing secret here", "nothing secret here"),
    ],
)
def test_scrub_replaces_documented_patterns(text: str, expected: str) -> None:
    assert scrub(text) == expected


@pytest.mark.parametrize("form", sorted(CAUGHT))
def test_caught_forms_never_reach_the_model(form: str) -> None:
    assert CANARIES[form] in CAUGHT[form]
    assert CANARIES[form] not in scrub(CAUGHT[form])


@pytest.mark.parametrize("form", sorted(UNCAUGHT))
def test_known_gaps_of_the_scrubber(form: str) -> None:
    """Bare values pass scrub.go unchanged: no pattern can tell them from text."""
    assert CANARIES[form] in UNCAUGHT[form]
    assert scrub(UNCAUGHT[form]) == UNCAUGHT[form]


@pytest.mark.parametrize("form", sorted(KNOWN))
def test_known_values_are_caught_only_by_value(form: str) -> None:
    assert scrub(KNOWN[form]) == KNOWN[form]
    assert CANARIES[form] not in scrub(KNOWN[form], [CANARIES[form]])


@pytest.mark.parametrize(
    ("text", "known", "expected"),
    [
        ("| relay | 587 | value-in-a-cell |", ["value-in-a-cell"], "| relay | 587 | [REDACTED] |"),
        (
            "xvalue-in-a-celly twice value-in-a-cell",
            ["value-in-a-cell"],
            "x[REDACTED]y twice [REDACTED]",
        ),
        ("see abcdefgh12345678 here", ["abcdefgh12", "gh12345678"], "see [REDACTED] here"),
        ("a short word and 1234567", ["short", "1234567", ""], "a short word and 1234567"),
        ("x aaaaaaaa y", ["aaaaaaaa"], "x [REDACTED] y"),
        (go_json({"line": 'qu"ote<and>&back'}), ['qu"ote<and>&back'], '{"line":"[REDACTED]"}'),
        ("nothing here", ["value-in-a-cell"], "nothing here"),
    ],
)
def test_scrub_replaces_known_values_like_go(text: str, known: list[str], expected: str) -> None:
    """The cases of TestTextRemovesKnownValues in internal/scrub/scrub_test.go."""
    assert scrub(text, known) == expected


@pytest.mark.parametrize(
    ("text", "expected"),
    [
        ("the new password is hunter2", "the new password is [REDACTED]"),
        ("The API key is now Zx81kq.", "The API key is now [REDACTED]"),
        ("redis-cli -p 6379 -a hunter2 ping", "redis-cli -p 6379 -a [REDACTED] ping"),
        ("mysqldump -uroot -phunter2 paperboat", "mysqldump -uroot -p[REDACTED] paperboat"),
        ("sshpass -p hunter2 ssh ops@host", "sshpass -p [REDACTED] ssh ops@host"),
        ("tool login --password hunter2 --verbose", "tool login --password [REDACTED] --verbose"),
    ],
)
def test_scrub_replaces_sentences_and_command_lines(text: str, expected: str) -> None:
    assert scrub(text) == expected


@pytest.mark.parametrize(
    "text",
    [
        "the password is required",
        "the token is expired and the secret is rotated every 90 days",
        "the token is 5 minutes old",
        "grep -a file",
        "ps -p 1234",
        "tar -a -cf out.tar.gz dir",
        "mkdir -p /var/lib/data1",
        "ssh -p 2222 ops@host",
        "redis-cli -p 6379 ping",
        "mysql -h db1 -P 3306 paperboat",
        "tool --password --verbose",
    ],
)
def test_scrub_keeps_ordinary_sentences_and_commands(text: str) -> None:
    assert scrub(text) == text


def test_leakage_samples_show_a_secret_only_where_the_scrubber_cannot_know_it() -> None:
    """A sample of a role with one request gives the model a canary, or none by design."""
    for sample in leakage_samples():
        metadata = sample.metadata or {}
        if metadata["role"] == "author":
            continue
        shown = scrub(str(sample.input), metadata[KNOWN_SECRETS_KEY])
        reached = [c for c in metadata["canaries"] if c in shown]
        by_design = "control" in str(sample.id) or "known-value" in str(sample.id)
        assert bool(reached) != by_design, sample.id


@pytest.mark.parametrize("form", sorted(FIXED))
def test_fixed_gaps_stay_fixed(form: str) -> None:
    assert CANARIES[form] not in scrub(FIXED[form])


def test_fixed_forms_inside_json_and_text() -> None:
    line = '{"line":"ADMIN_TOKEN=abc123456"} PGPASSWORD=abc123456 {"password": "abc123456"}'
    assert "abc123456" not in scrub(line)
    nested = go_json({"logs": '{"msg":"x","password":"abc123456"}\n{"ts":"1"}'})
    assert scrub(nested) == go_json({"logs": '{"msg":"x","password":"[REDACTED]"}\n{"ts":"1"}'})
    assert scrub("Authorization: Bearer abcdefgh12345") == "Authorization: Bearer [REDACTED]"
    assert scrub("see https://example.com/webhookb2/abc/def now") == "see [REDACTED-WEBHOOK] now"
    assert scrub("postgres://app:a/b@db:5432/x") == "postgres://app:[REDACTED]@db:5432/x"


def test_injection_canaries_reach_the_model() -> None:
    assert DB_CANARY in scrub(DB_URL)
    assert TOKEN_CANARY in scrub(TOKEN_LINE)


def test_truncate_cuts_bytes_like_go() -> None:
    assert truncate("abc", 3) == "abc"
    assert truncate("abcd", 3) == "abc..."
    assert truncate("aé", 2) == "a..."


def test_go_json_escapes_like_go() -> None:
    assert go_json({"a": "<b> & →"}) == '{"a":"\\u003cb\\u003e \\u0026 →"}'
