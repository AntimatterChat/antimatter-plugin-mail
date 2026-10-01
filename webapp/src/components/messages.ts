// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import {defineMessages} from 'react-intl';

export const messages = defineMessages({
    title: {id: 'mail.title', defaultMessage: 'Mail'},
    loading: {id: 'mail.loading', defaultMessage: 'Loading…'},
    retry: {id: 'mail.retry', defaultMessage: 'Try again'},

    // Account
    setupTitle: {id: 'mail.setup.title', defaultMessage: 'Connect your mailbox'},
    setupIntro: {id: 'mail.setup.intro', defaultMessage: 'Read and send your email from here. Your mail provider tells you the IMAP and SMTP servers to use; use an app password if it offers them.'},
    settingsTitle: {id: 'mail.settings.title', defaultMessage: 'Mail account'},
    email: {id: 'mail.settings.email', defaultMessage: 'Email address'},
    name: {id: 'mail.settings.name', defaultMessage: 'Your name'},
    incoming: {id: 'mail.settings.incoming', defaultMessage: 'Incoming mail (IMAP)'},
    outgoing: {id: 'mail.settings.outgoing', defaultMessage: 'Outgoing mail (SMTP)'},
    server: {id: 'mail.settings.server', defaultMessage: 'Server'},
    port: {id: 'mail.settings.port', defaultMessage: 'Port'},
    security: {id: 'mail.settings.security', defaultMessage: 'Security'},
    securityTLS: {id: 'mail.settings.security.tls', defaultMessage: 'SSL/TLS'},
    securityStartTLS: {id: 'mail.settings.security.starttls', defaultMessage: 'STARTTLS'},
    securityNone: {id: 'mail.settings.security.none', defaultMessage: 'None (unencrypted)'},
    username: {id: 'mail.settings.username', defaultMessage: 'Username'},
    password: {id: 'mail.settings.password', defaultMessage: 'Password'},
    passwordKeep: {id: 'mail.settings.password.keep', defaultMessage: 'Leave empty to keep the saved password'},
    smtpSame: {id: 'mail.settings.smtp_same', defaultMessage: 'Leave the username and password empty to use the IMAP ones.'},
    connect: {id: 'mail.settings.connect', defaultMessage: 'Connect'},
    connecting: {id: 'mail.settings.connecting', defaultMessage: 'Checking the servers…'},
    save: {id: 'mail.settings.save', defaultMessage: 'Save'},
    cancel: {id: 'mail.cancel', defaultMessage: 'Cancel'},
    disconnect: {id: 'mail.settings.disconnect', defaultMessage: 'Disconnect'},
    disconnectConfirm: {id: 'mail.settings.disconnect.confirm', defaultMessage: 'Disconnect your mailbox from Antimatter? Your mail stays on your mail server.'},
    passwordsStored: {id: 'mail.settings.passwords_stored', defaultMessage: 'Your passwords are stored encrypted on the Antimatter server.'},

    // List
    folder: {id: 'mail.folder', defaultMessage: 'Folder'},
    unreadCount: {id: 'mail.unread_count', defaultMessage: '{count} unread'},
    refresh: {id: 'mail.refresh', defaultMessage: 'Refresh'},
    settings: {id: 'mail.settings', defaultMessage: 'Account settings'},
    emptyFolder: {id: 'mail.empty_folder', defaultMessage: 'No messages here.'},
    loadMore: {id: 'mail.load_more', defaultMessage: 'Load older messages'},
    yesterday: {id: 'mail.yesterday', defaultMessage: 'Yesterday'},
    noSubject: {id: 'mail.no_subject', defaultMessage: '(no subject)'},
    unknownSender: {id: 'mail.unknown_sender', defaultMessage: '(unknown sender)'},
    unreadMessage: {id: 'mail.unread_message', defaultMessage: 'Unread'},
    hasAttachments: {id: 'mail.has_attachments', defaultMessage: 'Has attachments'},
    folderInbox: {id: 'mail.folder.inbox', defaultMessage: 'Inbox'},
    folderDrafts: {id: 'mail.folder.drafts', defaultMessage: 'Drafts'},
    folderSent: {id: 'mail.folder.sent', defaultMessage: 'Sent'},
    folderArchive: {id: 'mail.folder.archive', defaultMessage: 'Archive'},
    folderJunk: {id: 'mail.folder.junk', defaultMessage: 'Junk'},
    folderTrash: {id: 'mail.folder.trash', defaultMessage: 'Trash'},

    // Reading view
    back: {id: 'mail.back', defaultMessage: 'Back to the list'},
    to: {id: 'mail.to', defaultMessage: 'To'},
    cc: {id: 'mail.cc', defaultMessage: 'Cc'},
    markUnread: {id: 'mail.mark_unread', defaultMessage: 'Mark as unread'},
    moveTo: {id: 'mail.move_to', defaultMessage: 'Move to…'},
    delete: {id: 'mail.delete', defaultMessage: 'Delete'},
    deleteForever: {id: 'mail.delete_forever', defaultMessage: 'Delete this message for good? It is in the trash.'},
    remoteImagesBlocked: {id: 'mail.remote_images.blocked', defaultMessage: '{count, plural, one {# remote image was} other {# remote images were}} hidden to protect your privacy.'},
    remoteImagesShow: {id: 'mail.remote_images.show', defaultMessage: 'Show images'},
    attachments: {id: 'mail.attachments', defaultMessage: 'Attachments'},
    download: {id: 'mail.download', defaultMessage: 'Download {filename}'},
});
