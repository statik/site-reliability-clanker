# Paperboat Manager (iOS)

Paperboat Manager is a native SwiftUI app for the manager of Paperboat, the fish rental
business. It shows how the business application runs. It also lets the manager approve,
schedule, send back or reject the work that Site Reliability Clanker proposes.

The app uses only the HTTP API of the control plane (`docs/API.md`) and three endpoints of
Paperboat (`/api/stats`, `/api/version`, `/api/healthz`). It has no third-party dependencies.

## Requirements

- Xcode 26 at `/Applications/Xcode.app`, with an iOS 18 or later simulator.
- `xcodegen` (`brew install xcodegen`).
- The control plane on `http://127.0.0.1:7070` and Paperboat on `http://localhost:8088`.

The scripts set `DEVELOPER_DIR` to Xcode. You do not need to change `xcode-select`.

## Build and test

```sh
ios/build.sh
```

The script does these steps:

1. It generates `PaperboatManager.xcodeproj` from `project.yml`.
2. It builds the app and the test bundle for the simulator. Warnings are errors.
3. It boots the simulator and runs the unit tests in it.

The script runs the test bundle with the `xctest` agent of Xcode (`xcrun simctl spawn`). It
does not use `xcodebuild test`. On the Mac where this app was written, `xcodebuild test`
started the app and then waited for a test runner that never connected. The probable cause is
that the first-launch packages of Xcode are not installed there
(`xcodebuild -checkFirstLaunchStatus` fails), and only an administrator can install them.
This cause is not proven, and `xcodebuild test` was not seen to pass. The agent runs the same
test bundle and gives a non-zero exit code if a test fails.

Build output goes to `ios/.build`. The generated project and `ios/.build` are not in git.
The unit tests do not use the network. They read the JSON files in `Tests/Fixtures`, which
were captured from the live API. The file `work_item_destructive_synthetic.json` is
hand-written, because no live work item destroys a resource or has a deferred layer.

To use a different simulator, set `SIMULATOR`:

```sh
SIMULATOR="iPhone 17" ios/build.sh
```

## Run

```sh
ios/run.sh
```

The script boots the simulator, installs the app that `build.sh` made and starts it. The
simulator reaches the servers on your Mac through `localhost`.

Arguments after the script name go to the app. A Debug build accepts these arguments, so that
you can open a screen without taps:

| Argument | Effect |
|---|---|
| `-initialScreen home\|approvals\|activity\|ask\|access\|settings` | Opens that screen |
| `-workItem WI-0020` | Opens the approval page of that work item. With `-initialScreen activity` it opens the timeline. With `-initialScreen ask` it opens the scoped chat |
| `-debugUser alice` | Uses that demo user for this start only |
| `-debugScroll impact\|tier\|changes\|safety\|permission\|evidence\|approvers\|progress` | Scrolls the approval page to that section. Home accepts `environment`, `catchup`, `chain`. The Activity detail accepts `progress`, `jobs`, `timeline` |
| `-debugDecision choose\|approve\|schedule\|request_changes\|reject` | Opens that step of the decision |
| `-debugSubmit YES` | Sends the decision. It works only with `request_changes` and only if the user is a viewer. It shows how the server refuses |
| `-debugJob 42` | Opens the log of that job |
| `-debugSignIn YES` | Asks for a sign-in code when the sign-in screen opens |

Example:

```sh
ios/run.sh -initialScreen approvals -workItem WI-0020 -debugUser alice
```

## Screenshots

```sh
ios/scripts/screenshots.sh
```

The script starts the app on each main screen, in light and in dark mode, and saves the
pictures in `ios/screenshots`. It uses the live servers. All its requests are reads, with one
exception: it sends one `request_changes` decision as the viewer `dave`. The control plane
refuses that decision with status 403, and the screenshot shows the refusal.

## Configuration

Open Settings with the name button at the top right of each tab.

- **Signed in as**: with a control plane that has a sign-in, the person who signed in
  (from `GET /api/v1/me`) and the **Sign out** button. With the demo, the demo users from
  `GET /api/v1/users`. The default demo user is `dave`, a viewer.
- **Servers**: the addresses of the control plane and of Paperboat. The app keeps them in
  UserDefaults.

The app permits plain HTTP only for local-network addresses (`NSAllowsLocalNetworking`).

## Sign-in

The app asks the control plane if people sign in (`GET /api/v1/auth`). There are two
answers:

- **The demo** answers `{"sign_in": false}`. The app sends the name of the demo user in
  the `X-Clanker-User` header, as before.
- **A control plane with a sign-in** answers with status 401 until the app has a token.
  The app then shows the sign-in screen.

The sign-in is the OAuth 2.0 device flow of the control plane (`internal/signin/device.go`):

1. Tap **Sign in**. The app asks for a code (`POST /auth/device/code`) and shows it,
   together with the address of the page.
2. Open the page in a browser where you are signed in to the dashboard. The button
   **Open the page in the browser** opens it on the phone. Approve only if the page shows
   the same code.
3. The app asks for its token (`POST /auth/device/token`) at the interval that the server
   gives. If a request does not reach the server, the app tries again, because the phone
   can lose the connection while the browser is in front.
4. After the approval, the app shows who is signed in and loads the data.

If you refuse the code, or if nobody approves it in ten minutes, the app says so and you
can ask for a new code.

The token is a secret. The app keeps it in the Keychain, one token for each server address.
The token is not in UserDefaults and the app writes no log. The app sends the token as
`Authorization: Bearer` on each request to the control plane, also on the event stream. The
app does not send the token to Paperboat.

A token is valid for twelve hours. When the control plane answers with status 401, the app
removes the token and shows the sign-in screen with a message.

**Sign out** in Settings removes the token from the phone. The control plane has no
endpoint that ends one token. Thus a token stays valid on the server until its twelve
hours are over.

The Keychain accepts only a signed app. `project.yml` signs the app "to run locally", which
needs no development team. Without this signature the Keychain gives status -34018.

To try the sign-in in the simulator without taps, start the app with the address of a
control plane that has a sign-in:

```sh
ios/run.sh -controlPlaneURL http://127.0.0.1:7270 -debugSignIn YES
```

## Screens

- **Home**: the work items that need a decision, the business counters of Paperboat, the
  health and versions of each environment, the catch-up summary and the audit-chain status.
- **Approvals**: the approval page in the order of section 7.2 of `docs/PLAN.md`. Text that a
  model wrote has a label with its source. Counts and resources have a label that says they
  come from the saved plan.
- **Activity**: all work items with a filter on the state, and for each one the timeline,
  the jobs and the job logs.
- **Ask**: the chat, global or about one work item, with tappable sources.
- **Access**: the active grants, the identities of the agent and the recent grants.

## How a decision works

An approval is what releases the apply. There is no separate apply button. The approval is
bound to the commit and to the plan hashes that the page shows. If the plan changes, the
earlier approval does not count and the page says so.

Each decision has two steps. Step 1: choose approve, approve and schedule, request changes
or reject. Step 2: read what will run, add a comment if you want one, and confirm. If the
server refuses the decision, the app shows the message of the server without changes.

After an approval, the page shows the progress of each layer: the grant, the apply, the end
of the grant and the verification. These rows come from the event stream.

## Limits

- **No sign-in in the demo.** With a control plane that has no sign-in, the app sends the
  name that you select in the `X-Clanker-User` header. Anyone who can reach the server can
  do the same.
- **Sign out is local.** The control plane cannot end one token. See "Sign-in".
- **The sign-in was not tested on a real phone.** It ran in the simulator against a control
  plane with a fake identity provider. The button that opens the browser was not tapped.
- **Simulated clock.** The control plane of the prototype runs on a virtual clock. The app
  shows control-plane times in UTC. The schedule picker starts from the clock of the control
  plane, not from the clock of the phone.
- **Event stream.** The server does not replay events. After a reconnect the app gets the
  missed events from `GET /events?after=`. Each screen also has pull to refresh.
- **Grant lifetime.** The API does not state how long a new grant lasts. The app measures
  the most recent apply grant and shows that time.
- **iPhone portrait only.** There is no iPad layout and no landscape layout.
- **No notifications.** The app does not send push notifications when a decision is needed.
- **No "defer" action.** The API has no such decision. The page shows the consequence of
  waiting next to the choices.
- **No development team.** The app has only a signature "to run locally". It runs in the
  simulator only.

## Layout

```text
ios/
  project.yml       project description for xcodegen
  build.sh run.sh   scripts
  App/Core          API client, device flow, token store, event stream, settings, timestamps
  App/Models        Codable types of the API
  App/Logic         pure functions: approver progress, change summary, apply progress
  App/ViewModels    @Observable types, one for each screen
  App/Views         SwiftUI views
  Tests             unit tests and fixtures
  scripts           make_icon.swift draws the app icon; screenshots.sh takes the screenshots
  screenshots       each main screen in light and dark mode
```
