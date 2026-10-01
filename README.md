# Mail

An Antimatter plugin to read and send email without leaving Antimatter. Each user connects their
own mailbox over IMAP (reading) and SMTP (sending).

## Features

- **Mail in the app bar.** Mail opens its panel in the right-hand slot, with the inbox's unread count
  on its icon. Under the Fusion web UI the panel uses Fusion's own styles, as in its mockup; the
  classic web app gets a classic-styled version.
- **Your own mailbox.** Each user connects their mailbox over IMAP (reading) and SMTP (sending),
  from the panel. The servers must accept the credentials before they are saved; passwords are
  stored encrypted (AES-GCM, with a key the plugin keeps in its KV store) and never logged. Use an
  app password where your provider offers them.
- **Sign in with Google or Microsoft.** Once the admin sets up an OAuth client for them, Gmail,
  Google Workspace, Outlook.com and Microsoft 365 users connect their mailbox by signing in with
  their provider instead of typing passwords (see below). The plugin then signs in to IMAP and
  SMTP with access tokens (XOAUTH2); the refresh token is stored encrypted like passwords.
- **Read.** Folders, messages newest first with their snippet and unread state, and the reading view.
  HTML mail is sanitised on the server and shown in a sandboxed frame that runs no script; remote
  images are hidden until you ask for them. Attachments are downloaded, never opened in the app.
- **Organise.** Mark as unread, move to another folder, delete (to the trash, then for good).
  Select messages in the list to mark them as read or unread, or delete them, at once. Search a
  folder for the messages that contain a text (IMAP SEARCH, done by the mail server).
- **Write.** New messages, replies (to the sender or everyone, threaded with In-Reply-To and
  References) and forwards with their attachments (which can be removed), in plain text. Sent
  mail is kept in the sent folder.
- **Drafts.** The message being written is saved to the drafts folder (flagged `\Draft`) a few
  seconds after each change and when it's closed; the drafts folder is created if the server has
  none. Drafts open in the editor from the drafts folder, and sending one deletes it. Each save
  replaces the previous one, found by its Message-ID.
- **Share to a channel.** Post a summary of a message (sender, subject, date, the beginning of the
  text, attachment names) into a channel, with a comment. The message can't mention anyone.

The server watches the inbox of each online user and tells them when the unread count changes. It
keeps an IMAP IDLE connection to the inbox where the server supports IDLE, so new mail shows up
right away; it checks the other inboxes every few minutes (with STATUS, at most ten at once), and
falls back to that when IDLE fails or no connection slot is free. Requests reuse the connections of
recent requests for a few minutes. Each user has at most four connections open at once, and the
plugin two hundred overall, IDLE and unused connections included; IDLE takes at most half of them.

## Configuration

In **System Console > Plugins > Mail**:

- **Allow mail servers on private networks** (off): users can only connect to public addresses
  unless this is on, so they can't reach the internal services of the network the server runs in.
  Turn it on for a mail server on your internal network.
- **Allow unencrypted connections** (off): connections must use TLS or STARTTLS unless this is on.
- **Unread check interval** (2 minutes) and **Largest message size** (25 MB).

### Signing in with Google or Microsoft

Both providers send users back to the same redirect URI, under the plugin's route:

```
<Site URL>/plugins/com.antimatterchat.mail/oauth/callback
```

for example `https://chat.example.com/plugins/com.antimatterchat.mail/oauth/callback`. The Site
URL must be set in **System Console > Environment > Web Server**. Users get a "Sign in with"
button in the mail panel for each provider whose client ID and secret are set.

**Google** (Gmail and Google Workspace):

1. In the [Google Cloud console](https://console.cloud.google.com/), create a project (or pick
   one) and set up its **OAuth consent screen**: *Internal* for your Workspace users only, or
   *External* for any Google account. Add the scope `https://mail.google.com/` (and `openid`,
   `email`, `profile`).
2. Under **APIs & Services > Credentials**, create an **OAuth client ID** of type *Web
   application*, with the redirect URI above as an *Authorized redirect URI*.
3. Copy its client ID and secret to **Google OAuth client ID** and **Google OAuth client secret**.

`https://mail.google.com/` is a restricted scope: an *External* app in testing mode only works for
the test users you list, and its sign-ins expire after seven days; publishing it to more than a
hundred users needs Google's verification. *Internal* apps need neither. Workspace admins must
allow IMAP for their users.

**Microsoft** (Outlook.com and Microsoft 365):

1. In the [Microsoft Entra admin center](https://entra.microsoft.com/), under **App
   registrations**, register an application. Pick the supported account types that match the
   **Microsoft tenant** setting, and add the redirect URI above as a *Web* redirect URI.
2. Under **API permissions**, add the delegated permissions `IMAP.AccessAsUser.All` and
   `SMTP.Send` of *Office 365 Exchange Online* (under *APIs my organization uses*), and
   `offline_access`, `openid`, `email` and `profile` of *Microsoft Graph*. Grant admin consent if
   your users can't consent themselves.
3. Under **Certificates & secrets**, create a client secret, and copy the application (client) ID
   and the secret's value to **Microsoft OAuth client ID** and **Microsoft OAuth client secret**.
   Client secrets expire: renew it before it does.
4. Set **Microsoft tenant**: `common` (any account), `organizations`, `consumers`, or your
   tenant's ID or domain to only let your organization's accounts in.

Microsoft 365 turns off authenticated SMTP (SMTP AUTH) for many tenants and mailboxes: turn it on
for the mailboxes that send mail from Antimatter (`Set-CASMailbox <user> -SmtpClientAuthenticationDisabled $false`),
and make sure IMAP is allowed for them.

Disconnecting a mailbox forgets its sign-in; users can also revoke the access from their Google
or Microsoft account.

## Trying it out

Any IMAP/SMTP account works, e.g. a Gmail or Fastmail account with an app password
(`imap.gmail.com` 993 SSL/TLS, `smtp.gmail.com` 465 SSL/TLS or 587 STARTTLS).

For a local test server, [GreenMail](https://greenmail-mail-test.github.io/greenmail/) creates
mailboxes on the fly:

```sh
docker run --rm -p 3025:3025 -p 3143:3143 -p 3465:3465 -p 3993:3993 \
    -e GREENMAIL_OPTS='-Dgreenmail.setup.test.all -Dgreenmail.hostname=0.0.0.0 -Dgreenmail.auth.disabled' \
    greenmail/standalone
```

Turn on both admin settings above, then connect with any address (e.g. `ada@example.com`), server
`localhost`, IMAP port 3143 and SMTP port 3025, security *None*, any password. Mail sent to
`ada@example.com` lands in that inbox.

## Development

```sh
make dist      # build the plugin bundle in dist/
make test      # run the server and webapp tests
make check-style
```

Set `MM_SERVICESETTINGS_ENABLEDEVELOPER=true` to only build the server for the current platform.

## License

GNU Affero General Public License v3.0, see [LICENSE.txt](LICENSE.txt). The build tooling is derived
from Apache-2.0 licensed Mattermost plugins, see [NOTICE.txt](NOTICE.txt).
