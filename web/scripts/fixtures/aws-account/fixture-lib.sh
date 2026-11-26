# shellcheck shell=bash
# shellcheck disable=SC2154 # state comes from the stand-in that sources this file.
# Helpers of the stand-ins of this directory. The caller sets "state" to the directory of
# SRC_FIXTURE_STATE.

mkdir -p "${state}"

# fail_once fails the call once when it starts with the text of SRC_FIXTURE_FAIL, so that
# a test can stop a run at one step and check that the next run goes on from there.
fail_once() {
  local fail="${SRC_FIXTURE_FAIL:-}"
  if [ -n "${fail}" ] && [ "${1#"${fail}"}" != "$1" ] && [ ! -f "${state}/failed-once" ]; then
    touch "${state}/failed-once"
    echo "the stand-in fails once: $1" >&2
    exit 1
  fi
}

# count adds one to a counter of the state directory and prints it.
count() {
  local file="${state}/count-$1" value=0
  if [ -f "${file}" ]; then value="$(cat "${file}")"; fi
  value=$((value + 1))
  echo "${value}" >"${file}"
  echo "${value}"
}

# applied reports whether the home stack holds resources.
applied() {
  [ -f "${state}/home-applied" ]
}
