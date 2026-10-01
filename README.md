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
- **Read.** Folders, messages newest first with their snippet and unread state, and the reading view.
  HTML mail is sanitised on the server and shown in a sandboxed frame that runs no script; remote
  images are hidden until you ask for them. Attachments are downloaded, never opened in the app.
- **Organise.** Mark as unread, move to another folder, delete (to the trash, then for good).
- **Write.** New messages, replies (to the sender or everyone, threaded with In-Reply-To and
  References) and forwards, in plain text. Sent mail is kept in the sent folder.
- **Share to a channel.** Post a summary of a message (sender, subject, date, the beginning of the
  text, attachment names) into a channel, with a comment. The message can't mention anyone.

The server checks the inbox of each online user every few minutes (polling with STATUS, at most ten
at once) and tells them when the unread count changes.

## Configuration

In **System Console > Plugins > Mail**:

- **Allow mail servers on private networks** (off): users can only connect to public addresses
  unless this is on, so they can't reach the internal services of the network the server runs in.
  Turn it on for a mail server on your internal network.
- **Allow unencrypted connections** (off): connections must use TLS or STARTTLS unless this is on.
- **Unread check interval** (2 minutes) and **Largest message size** (25 MB).

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
