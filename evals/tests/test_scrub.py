"""Behaviour of the scrub mirror, and the secret forms that scrub.go does not catch."""

import pytest

from clanker_evals.gotext import go_json, scrub, truncate
from clanker_evals.tasks.injection import DB_CANARY, DB_URL, TOKEN_CANARY, TOKEN_LINE
from clanker_evals.tasks.secrets import CANARIES, CAUGHT, FIXED, UNCAUGHT


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
    """Prose and bare values pass scrub.go unchanged: no pattern can tell them from text."""
    assert CANARIES[form] in UNCAUGHT[form]
    assert CANARIES[form] in scrub(UNCAUGHT[form])


@pytest.mark.parametrize("form", sorted(FIXED))
def test_fixed_gaps_stay_fixed(form: str) -> None:
    assert CANARIES[form] not in scrub(FIXED[form])


def test_fixed_forms_inside_json_and_text() -> None:
    line = '{"line":"ADMIN_TOKEN=abc123456"} PGPASSWORD=abc123456 {"password": "abc123456"}'
    assert "abc123456" not in scrub(line)
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
