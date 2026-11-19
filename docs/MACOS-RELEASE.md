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
- When the app starts, it runs `clanker setup --no-browser` and shows the wizard in its
  window. The dashboard opens in a second window.
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

You need a membership in the Apple Developer Program.

1. Create a certificate of the type **Developer ID Application**. Only the Account Holder
   of the team can create one. In Xcode: Settings, Accounts, select the team, Manage
   Certificates, the plus button, Developer ID Application. The certificate and its
   private key are then in the login keychain.
2. Find the name of the identity:

   ```sh
   security find-identity -v -p codesigning
   ```

   The line starts with `Developer ID Application:` and ends with the team ID in
   parentheses.
3. Create an app-specific password for your Apple Account at
   <https://account.apple.com>, in Sign-In and Security.
4. Store the sign-in for the notary service in the keychain. The command asks for the
   Apple Account, the team ID and the app-specific password. The name of the profile is
   your choice:

   ```sh
   xcrun notarytool store-credentials src-notary
   ```

The certificate and the notary sign-in must be of the same team. Do not put the identity,
the team ID or the Apple Account in this repository.

### Each release

```sh
export CODESIGN_IDENTITY="Developer ID Application: <name> (<team ID>)"
export NOTARY_PROFILE="src-notary"
make dmg VERSION=v0.2.0
```

The script `scripts/macos/package.sh` then does these steps:

1. It signs `clanker` inside the app, then the app, with the hardened runtime and a
   secure timestamp.
2. It sends the app to the notary service, waits, and staples the ticket to the app.
3. It builds the image with `dmgbuild` (the version and the hashes are in
   `scripts/macos/dmg/requirements.txt`).
4. It signs the image, sends it to the notary service, and staples the ticket.
5. It asks Gatekeeper to accept the app and the image (`spctl`, `stapler validate`).

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
spctl --assess --type open --context context:primary-signature --verbose=2 SRC.dmg
xcrun stapler validate SRC.dmg
```

Open the image, move the app to Applications, and open the app with a double-click.

## Limits

- A forced quit of the app leaves the `clanker` program. Stop it with
  `pkill -INT -f "clanker setup"`.
- The app has no update function. A new release is a new download.
