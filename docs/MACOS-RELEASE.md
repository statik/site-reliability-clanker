# The macOS app and its disk image

`make dmg VERSION=v0.2.0` writes `dist/SRC.dmg`. The image has the app
`Site Reliability Clanker.app` for Apple silicon and for Intel, and a link to the
Applications folder.

## What the app is

The app is a small window around the `clanker` program.

- `Contents/MacOS/SRC` is the Swift program that macOS starts. The source is in
  `macos/App`.
- `Contents/MacOS/clanker` is the same program as `dist/clanker-darwin-*`, for both
  processor types.
- When the app starts, it runs `clanker setup --app` and shows the wizard in its window.
  The dashboard opens in a second window.
- When the setup is finished, a later start of the app starts the control plane and shows
  the dashboard. The wizard does not start again.
- The app gives the program a search path with the login shell path and the usual install
  directories (`/opt/homebrew/bin`, `/usr/local/bin`, `~/.docker/bin`). An app that Finder
  starts gets only the system directories, and then the wizard does not find `docker`.
- A closed window does not stop the control plane. A click on the Dock icon shows the
  window again. Quit (Command-Q) stops the program and the tools that it started.
- The output of the program is in `~/Library/Logs/Site Reliability Clanker/clanker.log`.
- The data directory is `~/.clanker`, the same as for the downloaded binary.

## A test image

With no settings, the script signs the app ad hoc and does not notarize it:

```sh
make dmg VERSION=v0.2.0
```

This image opens on the Mac that built it. Gatekeeper refuses it on other Macs. Do not
publish it.

## A signed and notarized image

### One-time setup

You need a membership in the Apple Developer Program, and Xcode in `/Applications`.

No certificate and no private key are on the build machine. Xcode asks Apple to sign with
the Developer ID certificate that Apple keeps for the team (a cloud-managed certificate).

1. Sign in to Xcode: Settings, Accounts, the plus button. The account must have the right
   to use cloud-managed Developer ID certificates in the team. The Account Holder has it
   and can give it to other members.
2. Create an app-specific password for your Apple Account at
   <https://account.apple.com>, in Sign-In and Security.
3. Store the sign-in for the notary service in the keychain. The command asks for the
   Apple Account, the team ID and the app-specific password. The name of the profile is
   your choice:

   ```sh
   xcrun notarytool store-credentials src-notary
   ```

The team of Xcode and the team of the notary sign-in must be the same. Do not put the team
ID or the Apple Account in this repository.

### Each release

```sh
export SIGNING_TEAM="<team ID>"
export NOTARY_PROFILE="src-notary"
make dmg VERSION=v0.2.0
```

The script `scripts/macos/package.sh` then does these steps:

1. It builds the app and signs it ad hoc with the hardened runtime.
2. It puts the app into an Xcode archive and lets `xcodebuild -exportArchive` sign it with
   the Developer ID method. It checks that the app and `clanker` inside it have the
   Developer ID authority, the team, the hardened runtime and a secure timestamp.
3. It sends the app to the notary service, waits, and staples the ticket to the app.
4. It builds the image with `dmgbuild` (the version and the hashes are in
   `scripts/macos/dmg/requirements.txt`).
5. It sends the image to the notary service and staples the ticket. The image has no
   signature of its own, because no private key is on the machine. The ticket is enough
   for Gatekeeper.
6. It validates the two tickets, copies the app out of the image, and asks Gatekeeper to
   accept the copy (`spctl`).

With `SIGNING_TEAM` only, the script signs and does not notarize. Use this to test the
sign-in of Xcode. Do not publish that image.

The first notarization of a team can take much longer than the later ones.

Upload `dist/SRC.dmg` to the release with that name. The name has no version, so this
address always gives the newest image:

```text
https://github.com/statik/site-reliability-clanker/releases/latest/download/SRC.dmg
```

`dist/checksums.txt` has the checksum of the image.

### A check as a person who downloads

Download the image with a browser, so that it has the quarantine mark. Then:

```sh
xcrun stapler validate SRC.dmg
```

Open the image, move the app to Applications, and open the app with a double-click.

## Limits

- After a forced quit of the app, the `clanker` program sees that the app is gone and
  stops by itself in some seconds. The next start of the app also ends a program that an
  earlier start left.
- To remove the local environment and start again, choose **Reset Demo Environment** in
  the S.R.C. menu. The app asks for a confirmation, runs `clanker reset` and shows the
  wizard from its first step. The reset removes the local cluster, the database of the
  control plane and the saved setup with the key of the model.
- The app has no update function. A new release is a new download.
