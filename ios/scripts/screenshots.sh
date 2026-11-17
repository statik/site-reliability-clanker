#!/usr/bin/env bash
# Takes a screenshot of every main screen against the live servers. The app has one theme.
# The log of a job is not in the set, because it shows file paths of the host.
# Run ./build.sh first. The only request that is not a read is one decision as the viewer
# "dave", which the control plane refuses: that screenshot shows the refusal.
set -euo pipefail

export DEVELOPER_DIR="${DEVELOPER_DIR:-/Applications/Xcode.app/Contents/Developer}"
SIMULATOR="${SIMULATOR:-iPhone 17 Pro}"
BUNDLE_ID="dev.clanker.paperboat-manager"
WAIT_SECONDS="${WAIT_SECONDS:-5}"

cd "$(dirname "$0")/.."
OUT="${OUT:-screenshots}"
APP=".build/Build/Products/Debug-iphonesimulator/PaperboatManager.app"
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

approval=(-debugUser alice -initialScreen approvals -workItem WI-0020)
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
  -workItem WI-0020 -debugDecision request_changes -debugSubmit YES
shot 12-blank-slate-changes -debugUser alice -workItem WI-0001 -debugScroll changes
shot 13-blank-slate-progress -debugUser alice -workItem WI-0001 -debugScroll progress
shot 14-activity -debugUser alice -initialScreen activity
shot 15-activity-rolled-back -debugUser alice -initialScreen activity -workItem WI-0005
shot 16-ask -debugUser alice -initialScreen ask
shot 17-ask-about-work-item -debugUser alice -initialScreen ask -workItem WI-0020
shot 18-access -debugUser alice -initialScreen access
shot 19-settings -debugUser alice -initialScreen settings
shot 20-home-environment -debugUser alice -initialScreen home -debugScroll environment
shot 21-home-audit-chain -debugUser alice -initialScreen home -debugScroll chain
shot 22-activity-jobs-timeline -debugUser alice -initialScreen activity \
  -workItem WI-0005 -debugScroll jobs

# Dynamic Type: the same screens with a large accessibility text size.
xcrun simctl ui "$SIMULATOR" content_size accessibility-large
shot 01-home-large-text -debugUser alice -initialScreen home
shot 03-approval-summary-large-text "${approval[@]}"
shot 09-decide-approve-confirm-large-text "${approval[@]}" -debugDecision approve
shot 14-activity-large-text -debugUser alice -initialScreen activity
xcrun simctl ui "$SIMULATOR" content_size large
xcrun simctl status_bar "$SIMULATOR" clear
xcrun simctl terminate "$SIMULATOR" "$BUNDLE_ID" >/dev/null 2>&1 || true
