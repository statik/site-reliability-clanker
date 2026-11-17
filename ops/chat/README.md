# Chat apps for approval in Slack and Microsoft Teams

With these apps a person approves or rejects a tier T2 change inside the chat. A tier T3
change shows its summary and a link, and is decided on the dashboard.

**Not tested against the real platforms.** The code was tested only with a fake Slack, a fake
Bot Connector and test keys. Nobody installed these manifests in a real workspace or tenant
yet. Expect to correct details at the first real installation.

Chat approval needs the sign-in of the control plane (`CLANKER_OIDC_ISSUER` and the other
sign-in settings), because a person links a chat account through it. The control plane
does not start when a chat app is configured and the sign-in is not.

The address of the control plane (`CLANKER_PUBLIC_URL`) must be reachable from the
internet over HTTPS: Slack and Microsoft send each click to it.

## How a person uses it

1. The app posts a request for approval in the channel.
2. The first time a person clicks Approve or Reject, nothing is decided. The person gets a
   private answer with a link. The link works once and for ten minutes.
3. The person opens the link, signs in to the control plane, checks the chat account and
   the email address on the page, and confirms.
4. The person clicks again. The decision is recorded as the user with that email address.
   The role of that user decides whether the person can approve.
5. The message changes for everybody: it shows who decided and when, and has no button.

A person sees and removes the linked accounts at `CLANKER_PUBLIC_URL/auth/chat-link`.

A rejection in chat has a fixed comment, because a chat button has no text field. To give
a reason, reject on the dashboard.

## Slack

1. Open https://api.slack.com/apps and select **Create New App**, then **From a manifest**.
2. Paste `slack/manifest.yaml`. Replace `clanker.example` with the host of
   `CLANKER_PUBLIC_URL`.
3. Install the app to the workspace.
4. Invite the app to the channel for the requests.
5. Give the control plane these settings:

| Setting | Value |
|---|---|
| `CLANKER_SLACK_SIGNING_SECRET` | **Basic Information**, **Signing Secret**. A secret |
| `CLANKER_SLACK_BOT_TOKEN` | **OAuth & Permissions**, **Bot User OAuth Token**. A secret |
| `CLANKER_SLACK_CHANNEL` | The ID of the channel, for example `C0123456789` |

With these three settings the app takes the place of `CLANKER_SLACK_WEBHOOK`: every Slack
notification goes through the app.

Limits: the app must be installed in one workspace. An installation for a whole Enterprise
Grid organization is refused.

## Microsoft Teams

1. Create an **Azure Bot** resource with the app type **Single Tenant**. Microsoft does not
   let you create a multi-tenant bot any more.
2. Set its messaging endpoint to `CLANKER_PUBLIC_URL/chat/teams/messages`.
3. Add the channel **Microsoft Teams** to the bot.
4. Create a client secret for the app registration of the bot.
5. Give the control plane these settings:

| Setting | Value |
|---|---|
| `CLANKER_TEAMS_APP_ID` | The Microsoft app ID of the bot |
| `CLANKER_TEAMS_APP_SECRET` | The client secret. A secret |
| `CLANKER_TEAMS_TENANT_ID` | The ID of the Microsoft Entra tenant |

6. Build the app package:
   - In `teams/manifest.json`, replace both `REPLACE-WITH-CLANKER_TEAMS_APP_ID` values with
     the app ID, and replace the name and the three addresses of the developer.
   - Add two icons to the `teams` directory. This repository has none, because the
     product has no brand assets for Teams. `color.png` is a full color PNG of 192 by 192
     pixels. `outline.png` is a PNG of 32 by 32 pixels with a white outline on a
     transparent background.
   - Make a zip file with `manifest.json` and the two icons at the top level:
     `cd ops/chat/teams && zip clanker-teams.zip manifest.json color.png outline.png`
7. In Teams, select **Apps**, **Manage your apps**, **Upload an app**, **Upload a custom
   app**, and select the zip file. An admin of the tenant must allow custom apps first.
8. Add the app to the team and the channel for the requests. The bot learns the channel
   from this step. Until then, each Teams notification fails with a message that says so.

With the three settings the bot takes the place of `CLANKER_TEAMS_WEBHOOK`.

Limits:

- The bot posts to one channel: the one where the app was added last.
- The bot cannot post in a private channel.
- These points come from the documentation and from memory, and no real tenant confirmed
  them: the form of the request that starts a thread in a channel, that a click on a card
  reaches the bot with the Entra object ID and the tenant ID of the person, and whether
  the manifest needs `validDomains` for the link button.

## Secrets

The signing secret, the bot token and the app secret are read from the environment. This is
for a laptop. An image for a cloud account must not carry them in `Env`: there they must
come from a secret store (issue #40).
