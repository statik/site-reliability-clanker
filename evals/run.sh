#!/usr/bin/env bash
# Runs the whole eval suite and writes the Inspect logs to evals/logs/<mode>/.
#
#   ./run.sh mock            every task with mockllm/model, no network
#   ./run.sh live            every task with the production model of each role
#   ./run.sh live --limit 2  more arguments go to each `inspect eval` call
#   ./run.sh rescore         score the logs in logs/live again, without new answers
#
# Models (override with the environment variables):
#   CLANKER_AUTHOR_MODEL, CLANKER_INVESTIGATOR_MODEL, CLANKER_EXPLAINER_MODEL,
#   CLANKER_REVIEWER_MODEL, CLANKER_GRADER_MODEL
# CLANKER_ADVERSARIAL_EPOCHS (default 3) repeats each sample of prompt_injection and
# secret_leakage, because one attempt does not show how often an attack works.
# The key is read from OPENROUTER_API_KEY, or from the file CLANKER_ENV_FILE, or from .env
# at the root of the checkout. It is never printed.
set -euo pipefail

cd "$(dirname "$0")"
mode="${1:-live}"
shift || true
extra=("$@")

tasks_dir="src/clanker_evals/tasks"
epochs="${CLANKER_ADVERSARIAL_EPOCHS:-3}"
log_dir="logs/${mode}"

read_key_from() {
  local file="$1" line
  [[ -f "$file" ]] || return 1
  line="$(grep -m1 '^OPENROUTER_API_KEY=' "$file" || true)"
  [[ -n "$line" ]] || return 1
  line="${line#OPENROUTER_API_KEY=}"
  line="${line%\"}"
  line="${line#\"}"
  export OPENROUTER_API_KEY="$line"
}

load_key() {
  [[ -n "${OPENROUTER_API_KEY:-}" ]] && return 0
  local common
  common="$(git rev-parse --path-format=absolute --git-common-dir)"
  read_key_from "${CLANKER_ENV_FILE:-/nonexistent}" ||
    read_key_from "../.env" ||
    read_key_from "${common%/.git}/.env" || {
    echo "error: OPENROUTER_API_KEY is not set. Put it in .env at the root of the" \
      "checkout, or set CLANKER_ENV_FILE to a file that has it." >&2
    exit 1
  }
}

case "$mode" in
mock)
  author="mockllm/model"
  investigator="mockllm/model"
  explainer="mockllm/model"
  reviewer="mockllm/model"
  grader="mockllm/model"
  ;;
rescore)
  load_key
  for log in logs/live/*.eval; do
    uv run inspect score "$log" --action overwrite --overwrite --display plain
  done
  uv run python report.py logs/live --out RESULTS.md
  exit 0
  ;;
live)
  load_key
  author="${CLANKER_AUTHOR_MODEL:-openrouter/anthropic/claude-sonnet-5.5}"
  investigator="${CLANKER_INVESTIGATOR_MODEL:-openrouter/anthropic/claude-sonnet-5.5}"
  explainer="${CLANKER_EXPLAINER_MODEL:-openrouter/anthropic/claude-haiku-5.5}"
  reviewer="${CLANKER_REVIEWER_MODEL:-openrouter/anthropic/claude-sonnet-5.5}"
  grader="${CLANKER_GRADER_MODEL:-openrouter/anthropic/claude-haiku-5.5}"
  ;;
*)
  echo "error: mode '${mode}' does not exist. Use 'mock', 'live' or 'rescore'." >&2
  exit 1
  ;;
esac

model_of() {
  case "$1" in
  author) echo "$author" ;;
  investigator) echo "$investigator" ;;
  explainer | ask) echo "$explainer" ;;
  reviewer) echo "$reviewer" ;;
  esac
}

# run <file> <task> <model> [inspect arguments]
run() {
  local file="$1" name="$2" model="$3"
  shift 3
  echo "== ${name} $* (${model})"
  uv run inspect eval "${tasks_dir}/${file}.py@${name}" --model "$model" \
    --model-role "grader=${grader}" --log-dir "$log_dir" --display plain \
    --no-fail-on-error "$@" ${extra[@]+"${extra[@]}"}
}

run author author_correctness "$author"
run author author_minimality_and_refusal "$author"
run investigator investigator_diagnosis "$investigator"
run explainer explainer_faithfulness "$explainer"
run explainer explainer_no_self_classification "$explainer"
run ask ask_groundedness "$explainer"
run reviewer reviewer_risk_detection "$reviewer"
for role in author investigator explainer ask reviewer; do
  run injection prompt_injection "$(model_of "$role")" -T "role=${role}" --epochs "$epochs"
  run secrets secret_leakage "$(model_of "$role")" -T "role=${role}" --epochs "$epochs"
done
run tier_policy tier_policy_regression "mockllm/model"

uv run python report.py "$log_dir" --out "$([[ "$mode" == live ]] && echo RESULTS.md || echo "${log_dir}/RESULTS.md")"
