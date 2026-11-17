#!/usr/bin/env bash
# Takes a screenshot of every main screen against the live servers. The app has one theme.
# The log of a job is not in the set, because it shows file paths of the host.
# Run ./build.sh first. The only request that is not a read is one decision as the viewer
# "dave", which the control plane refuses: that screenshot shows the refusal.
# The script asks the control plane which work items exist (GET /api/v1/work-items) and
# selects the ones to show by state and kind. Needs curl and python3.
set -euo pipefail

export DEVELOPER_DIR="${DEVELOPER_DIR:-/Applications/Xcode.app/Contents/Developer}"
SIMULATOR="${SIMULATOR:-iPhone 17 Pro}"
BUNDLE_ID="dev.clanker.src"
WAIT_SECONDS="${WAIT_SECONDS:-5}"
CONTROL_PLANE="${CONTROL_PLANE:-http://127.0.0.1:7070}"

cd "$(dirname "$0")/.."
OUT="${OUT:-screenshots}"
APP=".build/Build/Products/Debug-iphonesimulator/SRC.app"
mkdir -p "$OUT"

xcrun simctl bootstatus "$SIMULATOR" -b >/dev/null
xcrun simctl install "$SIMULATOR" "$APP"
xcrun simctl status_bar "$SIMULATOR" override --time "9:41" --batteryState charged \
  --batteryLevel 100 --cellularBars 4

# shot NAME ARGUMENTS...: starts the app with the arguments and saves NAME.png.
shot() {
  local name="$1"
  shift
  xcrun simctl terminate "$SIMULATOR" "$BUNDLE_ID" >/dev/null 2>&1 || true
  xcrun simctl launch "$SIMULATOR" "$BUNDLE_ID" "$@" >/dev/null
  sleep "$WAIT_SECONDS"
  xcrun simctl io "$SIMULATOR" screenshot "$OUT/${name}.png" >/dev/null 2>&1
  echo "$OUT/${name}.png"
}

# find_item FIELD VALUE...: prints the id of the newest work item whose FIELD has the first
# VALUE that any work item has. Fails if no work item has one of the values.
find_item() {
  python3 -c '
import json, sys
field, wanted, items = sys.argv[1], sys.argv[2:], json.load(sys.stdin)
for value in wanted:
    ids = sorted(item["id"] for item in items if item.get(field) == value)
    if ids:
        print(ids[-1])
        sys.exit(0)
sys.exit("The control plane has no work item with " + field + " " + " or ".join(wanted) + ".")
' "$@" <<<"$WORK_ITEMS"
}

if ! WORK_ITEMS="$(curl --silent --show-error --fail --max-time 10 \
  "$CONTROL_PLANE/api/v1/work-items")"; then
  echo "The control plane at $CONTROL_PLANE did not give the work items. Is it up?" >&2
  exit 1
fi
# The approval page is best with a work item that waits for a decision. Without one, the
# newest finished work item shows the same sections.
APPROVAL_ITEM="$(find_item state AwaitingApproval Done)"
BLANK_SLATE_ITEM="$(find_item kind blank-slate)"
ROLLED_BACK_ITEM="$(find_item state RolledBack Failed)"
echo "Approval page: $APPROVAL_ITEM. Blank slate: $BLANK_SLATE_ITEM."
echo "Rolled back: $ROLLED_BACK_ITEM."

approval=(-debugUser alice -initialScreen approvals -workItem "$APPROVAL_ITEM")
shot 01-home -debugUser alice -initialScreen home
shot 02-approvals -debugUser alice -initialScreen approvals
shot 03-approval-summary "${approval[@]}"
shot 04-approval-tier-changes "${approval[@]}" -debugScroll tier
shot 05-approval-safety "${approval[@]}" -debugScroll safety
shot 06-approval-permission-evidence "${approval[@]}" -debugScroll permission
shot 07-approval-approvers-progress "${approval[@]}" -debugScroll approvers
shot 08-decide-choose "${approval[@]}" -debugDecision choose
shot 09-decide-approve-confirm "${approval[@]}" -debugDecision approve
shot 10-decide-schedule "${approval[@]}" -debugDecision schedule
shot 11-decide-refused-for-viewer -debugUser dave -initialScreen approvals \
  -workItem "$APPROVAL_ITEM" -debugDecision request_changes -debugSubmit YES
shot 12-blank-slate-changes -debugUser alice -workItem "$BLANK_SLATE_ITEM" -debugScroll changes
shot 13-blank-slate-progress -debugUser alice -workItem "$BLANK_SLATE_ITEM" -debugScroll progress
shot 14-activity -debugUser alice -initialScreen activity
shot 15-activity-rolled-back -debugUser alice -initialScreen activity -workItem "$ROLLED_BACK_ITEM"
shot 16-ask -debugUser alice -initialScreen ask
shot 17-ask-about-work-item -debugUser alice -initialScreen ask -workItem "$APPROVAL_ITEM"
shot 18-access -debugUser alice -initialScreen access
shot 19-settings -debugUser alice -initialScreen settings
shot 20-home-environment -debugUser alice -initialScreen home -debugScroll environment
shot 21-home-audit-chain -debugUser alice -initialScreen home -debugScroll chain
shot 22-activity-jobs-timeline -debugUser alice -initialScreen activity \
  -workItem "$ROLLED_BACK_ITEM" -debugScroll jobs

# Dynamic Type: the same screens with a large accessibility text size.
xcrun simctl ui "$SIMULATOR" content_size accessibility-large
shot 01-home-large-text -debugUser alice -initialScreen home
shot 03-approval-summary-large-text "${approval[@]}"
shot 09-decide-approve-confirm-large-text "${approval[@]}" -debugDecision approve
shot 14-activity-large-text -debugUser alice -initialScreen activity
xcrun simctl ui "$SIMULATOR" content_size large
xcrun simctl status_bar "$SIMULATOR" clear
xcrun simctl terminate "$SIMULATOR" "$BUNDLE_ID" >/dev/null 2>&1 || true
