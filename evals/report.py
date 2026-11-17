"""Read the Inspect logs of a run, print a table and write RESULTS.md.

Usage: uv run python report.py [LOG_DIR] [--earlier LABEL=DIR]... [--repeats DIR]

For each task, role and model the newest log counts. The score of a task is the share of
samples that pass every check of the task (all_pass). With --earlier, a task that has no
log in LOG_DIR is taken from the newest earlier round that has it, and a table compares the
rounds. With --repeats, the logs of repeated single samples get their own section.
"""

import argparse
from collections import defaultdict
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, cast

from inspect_ai.log import EvalLog, list_eval_logs, read_eval_log

HERE = Path(__file__).resolve().parent
# OpenRouter list prices in USD for one million tokens (input, output), read on 2026-11-16.
# They are used only when a log has no cost from the provider.
PRICES = {
    "openrouter/anthropic/claude-haiku-5.5": (0.10, 0.50),
    "openrouter/anthropic/claude-sonnet-5.5": (2.00, 10.00),
}
CACHE_READ, CACHE_WRITE = 0.1, 1.25
SUMMARY_KEYS = {
    "all_pass",
    "attack_success",
    "leak",
    "canary_reached_model",
    "failed_reads",
    "risk_missed",
    "false_raise",
}
MILLION = 1_000_000
NOTE_LIMIT = 500
MIN_ROUNDS = 2
COMPARISON_NOTE = [
    "Between the rounds the runtime changed its prompts, its input frame and its scrubber.",
    "The column Measured of the first table names the round of each row. A task that was",
    "not run again in a later round describes the runtime of its own round.",
]
COMPARED = {
    "all_pass": "score",
    "attack_success": "attack success",
    "leak": "leak rate",
    "risk_missed": "miss rate on hidden risks",
    "false_raise": "false-raise rate on benign changes",
    "canary_reached_model": "runs where a canary passed the scrubber",
    "failed_reads": "failed `read_file` calls for each run, mean",
    "mitigation_fits_cause": "mitigation fits the cause",
    "model_graded_qa": "grader accuracy",
}


@dataclass
class Row:
    """The result of one task for one model."""

    task: str
    model: str
    samples: int
    epochs: int
    metrics: dict[str, float]
    failures: list[tuple[str, str]] = field(default_factory=list)
    measured: str = ""

    def cell(self, key: str) -> str:
        """Return a metric as text, or a dash when the task does not have it."""
        return f"{self.metrics[key]:.2f}" if key in self.metrics else "-"


def task_label(log: EvalLog) -> str:
    """Return the task name with its role argument, for example prompt_injection[author]."""
    name = log.eval.task.rsplit("/", 1)[-1]
    role = (log.eval.task_args or {}).get("role")
    return f"{name}[{role}]" if role else name


def newest_logs(log_dir: Path) -> list[EvalLog]:
    """Return the newest successful log for each task, role and model."""
    newest: dict[tuple[str, str], EvalLog] = {}
    for info in list_eval_logs(str(log_dir), recursive=False):
        log = read_eval_log(info)
        if log.status != "success":
            continue
        key = (task_label(log), log.eval.model)
        if key not in newest or log.eval.created > newest[key].eval.created:
            newest[key] = log
    return [newest[key] for key in sorted(newest)]


def row_of(log: EvalLog) -> Row:
    """Summarise one log: its metrics and the samples that failed a check."""
    metrics: dict[str, float] = {}
    for score in log.results.scores if log.results else []:
        for name, metric in score.metrics.items():
            if name in ("mean", "accuracy"):
                metrics[score.name] = float(metric.value)
    failures = []
    for sample in log.samples or []:
        for score in (sample.scores or {}).values():
            if not isinstance(score.value, dict):
                continue
            checks = cast("dict[str, Any]", score.value)
            if checks.get("all_pass") == 0:
                failures.append((str(sample.id), score.explanation or ""))
    runs = len(log.samples or [])
    return Row(
        task_label(log), log.eval.model, runs, log.eval.config.epochs or 1, metrics, failures
    )


def usage_lines(logs: list[EvalLog]) -> tuple[list[str], float]:
    """Return a table of tokens and cost for each model, and the total cost in USD."""
    tokens: dict[str, list[float]] = defaultdict(lambda: [0, 0, 0, 0.0])
    for log in logs:
        for model, usage in log.stats.model_usage.items():
            price_in, price_out = PRICES.get(model, (0.0, 0.0))
            cached = usage.input_tokens_cache_read or 0
            written = usage.input_tokens_cache_write or 0
            weighted = usage.input_tokens + CACHE_READ * cached + CACHE_WRITE * written
            listed = (weighted * price_in + usage.output_tokens * price_out) / MILLION
            entry = tokens[model]
            entry[0] += usage.input_tokens + written
            entry[1] += cached
            entry[2] += usage.output_tokens
            entry[3] += usage.total_cost if usage.total_cost is not None else listed
    lines = [
        "| Model | Input tokens | Cached input tokens | Output tokens | Cost (USD) |",
        "|---|---:|---:|---:|---:|",
    ]
    for model, (tokens_in, cached, tokens_out, cost) in sorted(tokens.items()):
        cells = f"{int(tokens_in):,} | {int(cached):,} | {int(tokens_out):,} | {cost:.4f}"
        lines.append(f"| {model} | {cells} |")
    return lines, sum(entry[3] for entry in tokens.values())


def summary_lines(rows: list[Row]) -> list[str]:
    """Return the main table: one line for each task and model."""
    lines = [
        "| Task | Model | Measured | Runs | Epochs | Score (all checks) | Attack success "
        "| Leak rate | Miss rate | False-raise rate | Grader accuracy |",
        "|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|",
    ]
    for row in rows:
        cells = [row.cell("all_pass"), row.cell("attack_success"), row.cell("leak")]
        cells += [row.cell("risk_missed"), row.cell("false_raise"), row.cell("model_graded_qa")]
        head = f"| {row.task} | {row.model} | {row.measured} | {row.samples} | {row.epochs} | "
        lines.append(head + " | ".join(cells) + " |")
    return lines


def comparison_lines(rounds: list[tuple[str, list[Row]]]) -> list[str]:
    """Return a table of the tasks that were measured in more than one round."""
    labels = [label for label, _ in rounds]
    lines = ["| Task | Metric | " + " | ".join(labels) + " |", "|---|---|" + "---:|" * len(labels)]
    tables = [{(row.task, row.model): row for row in rows} for _, rows in rounds]
    for key in sorted({key for table in tables for key in table}):
        found = [table.get(key) for table in tables]
        if sum(row is not None for row in found) < MIN_ROUNDS:
            continue
        runs = " | ".join(str(row.samples) if row else "-" for row in found)
        lines.append(f"| {key[0]} | runs | {runs} |")
        for metric, name in COMPARED.items():
            if any(row and metric in row.metrics for row in found):
                cells = " | ".join(row.cell(metric) if row else "-" for row in found)
                lines.append(f"| {key[0]} | {name} | {cells} |")
    return lines


def repeat_lines(logs: list[EvalLog]) -> list[str]:
    """Return one line for each sample that was repeated: how many of its runs passed."""
    lines = []
    for log in logs:
        counts: dict[str, list[int]] = defaultdict(lambda: [0, 0])
        for sample in log.samples or []:
            for score in (sample.scores or {}).values():
                if isinstance(score.value, dict):
                    checks = cast("dict[str, Any]", score.value)
                    counts[str(sample.id)][0] += int(checks.get("all_pass") == 1)
                    counts[str(sample.id)][1] += 1
        for sample_id, (passed, runs) in sorted(counts.items()):
            lines.append(
                f"- `{task_label(log)}` sample `{sample_id}`: {passed} of {runs} runs pass"
            )
    return lines


def review_rate_lines(row: Row) -> list[str]:
    """Return the miss rate and the false-raise rate of a Reviewer task, when it has them."""
    lines = []
    if "risk_missed" in row.metrics:
        missed = row.metrics["risk_missed"]
        lines.append(f"\nHidden-risk runs where the review did not raise the tier: {missed:.0%}.")
    if "false_raise" in row.metrics:
        raised = row.metrics["false_raise"]
        lines.append(f"\nBenign runs where the review raised the tier: {raised:.0%}.")
    return lines


def detail_lines(rows: list[Row]) -> list[str]:
    """Return the checks of each task and the samples that failed."""
    lines: list[str] = []
    for row in rows:
        checks = {k: v for k, v in row.metrics.items() if k not in SUMMARY_KEYS}
        lines += ["", f"### {row.task} ({row.model})", ""]
        lines.append(", ".join(f"{name} {value:.2f}" for name, value in checks.items()))
        if "attack_success" in row.metrics:
            attacks = round(row.metrics["attack_success"] * row.samples)
            lines.append(f"\nAttacks that succeeded: {attacks} of {row.samples} runs.")
        if "failed_reads" in row.metrics:
            reads = row.metrics["failed_reads"]
            lines.append(f"\nFailed `read_file` calls for each sample, mean: {reads:.1f}.")
        if "leak" in row.metrics:
            leaks = round(row.metrics["leak"] * row.samples)
            lines.append(f"\nRuns that leaked a canary: {leaks} of {row.samples}.")
        if "canary_reached_model" in row.metrics:
            reached = row.metrics["canary_reached_model"]
            lines.append(f"\nA canary passed the scrubber in {reached:.0%} of the runs.")
        lines += review_rate_lines(row)
        if row.failures:
            lines += ["", "Samples that failed a check:", ""]
            for sample, note in row.failures:
                lines.append(f"- `{sample}`: {' '.join(note.split())[:NOTE_LIMIT]}")
    return lines


def merged_rows(rounds: list[tuple[str, list[EvalLog]]]) -> tuple[list[Row], list[EvalLog]]:
    """Return one row for each task from the newest round that has it, and the logs used."""
    rows: dict[tuple[str, str], Row] = {}
    used: dict[tuple[str, str], EvalLog] = {}
    for label, logs in rounds:
        for log in logs:
            row = row_of(log)
            row.measured = label if len(rounds) > 1 else ""
            rows[(row.task, row.model)] = row
            used[(row.task, row.model)] = log
    return [rows[key] for key in sorted(rows)], [used[key] for key in sorted(used)]


def render(rounds: list[tuple[str, list[EvalLog]]], repeats: list[EvalLog]) -> str:
    """Return the text of RESULTS.md.

    Args:
        rounds: The label and the logs of each round, the oldest first. The last is current.
        repeats: Logs of single samples that were repeated; they get their own section.
    """
    rows, used = merged_rows(rounds)
    usage, cost = usage_lines(used)
    compared = ["", "## Rounds compared", "", *COMPARISON_NOTE, ""]
    compared += comparison_lines([(label, [row_of(log) for log in logs]) for label, logs in rounds])
    repeated = ["", "## Repeated samples", "", *repeat_lines(repeats)]
    dates = sorted(log.eval.created[:10] for log in used)
    parts = [
        "# Eval results",
        "",
        f"Written by `report.py` from {len(used)} Inspect logs, run on {dates[-1]}.",
        "A run is one sample in one epoch. The score is the share of runs that pass every",
        "check of the task. Attack success and leak rate are shares of runs. The miss rate",
        "is the share of hidden-risk runs where the review did not raise the tier. The",
        "false-raise rate is the share of benign runs where the review raised the tier. The",
        "grader accuracy is the model-graded rubric of the task; it is not part of the score.",
        "",
        *summary_lines(rows),
        *(compared if len(rounds) > 1 else []),
        *(repeated if repeats else []),
        "",
        "## Token usage",
        "",
        *usage,
        "",
        f"Cost of the logs in the first table at list price: {cost:.4f} USD. Runs that a newer",
        "log replaced, repeated samples, and calls of the grader in `run.sh rescore` are not",
        "in this sum.",
        "",
        "## Checks and failed samples",
        *detail_lines(rows),
        "",
    ]
    return "\n".join(parts)


def labelled(value: str) -> tuple[str, Path]:
    """Read a LABEL=DIR argument."""
    label, separator, directory = value.partition("=")
    if not separator or not label or not directory:
        raise argparse.ArgumentTypeError(
            f"{value!r} is not LABEL=DIR, for example 'round 1=logs/a'"
        )
    return label, Path(directory)


def main() -> None:
    """Print the summary table and write the results file."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("log_dir", nargs="?", default=str(HERE / "logs" / "live"))
    parser.add_argument("--out", default=str(HERE / "RESULTS.md"))
    parser.add_argument(
        "--earlier",
        type=labelled,
        action="append",
        default=[],
        metavar="LABEL=DIR",
        help="logs of an earlier round, oldest first; give it once for each round",
    )
    parser.add_argument("--label", default="now", help="label of LOG_DIR in the comparison")
    parser.add_argument("--repeats", help="directory with logs of repeated single samples")
    args = parser.parse_args()
    logs = newest_logs(Path(args.log_dir))
    if not logs:
        raise SystemExit(f"no successful Inspect log is in {args.log_dir}; run ./run.sh first")
    rounds = [(label, newest_logs(directory)) for label, directory in args.earlier]
    repeats = newest_logs(Path(args.repeats)) if args.repeats else []
    text = render([*rounds, (args.label, logs)], repeats)
    Path(args.out).write_text(text, encoding="utf-8")
    print(text.split("## Checks and failed samples", maxsplit=1)[0])
    print(f"Wrote {Path(args.out).name}")


if __name__ == "__main__":
    main()
