// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import {Client4} from 'mattermost-redux/client';

import manifest from './manifest';

export type Security = 'tls' | 'starttls' | 'none';

export type ServerSettings = {
    host: string;
    port: number;
    security: Security;
    username: string;
};

export type Account = {
    email: string;
    name: string;
    imap: ServerSettings;
    smtp: ServerSettings;

    // The provider the user signed in with, for accounts without passwords
    oauth_provider?: string;
};

export type AccountRequest = Account & {
    imap_password: string;
    smtp_password: string;
};

export type OAuthProvider = {
    id: string;
    name: string;
};

export type ClientConfig = {
    allow_insecure_connections: boolean;
    oauth_providers: OAuthProvider[];
};

export type FolderRole = 'inbox' | 'drafts' | 'sent' | 'archive' | 'junk' | 'trash' | '';

export type Folder = {
    name: string;
    delimiter?: string;
    role?: FolderRole;
    total: number;
    unread: number;
};

export type Address = {
    name?: string;
    email: string;
};

export type MessageSummary = {
    uid: number;
    from: Address[];
    to: Address[];
    subject: string;
    date: number;
    size: number;
    unread: boolean;
    flagged: boolean;
    answered: boolean;
    draft: boolean;
    has_attachments: boolean;
    snippet: string;
};

export type MessagePage = {
    folder: string;
    total: number;
    page: number;
    per_page: number;
    messages: MessageSummary[];
};

export type Attachment = {
    id: string;
    filename: string;
    content_type: string;
    size: number;
};

export type FullMessage = {
    folder: string;
    uid: number;
    message_id: string;
    in_reply_to: string[];
    references: string[];
    from: Address[];
    to: Address[];
    cc: Address[];
    bcc: Address[];
    reply_to: Address[];
    subject: string;
    date: number;
    unread: boolean;
    flagged: boolean;
    draft: boolean;
    text: string;
    html?: string;
    remote_images: number;
    attachments: Attachment[];
};

export type OutgoingMessage = {
    to: Address[];
    cc: Address[];
    bcc: Address[];
    subject: string;
    text: string;
    in_reply_to?: string;
    references?: string[];
    reply_to_message?: {folder: string; uid: number};

    // Identifies a draft across its saves; sending the message deletes the draft
    message_id?: string;
};

export type SavedDraft = {
    folder: string;
    uid: number;
    message_id: string;
};

export class ClientError extends Error {
    status: number;

    constructor(message: string, status: number) {
        super(message);
        this.status = status;
    }
}

export function apiURL(path: string) {
    return `${window.basename || ''}/plugins/${manifest.id}/api/v1${path}`;
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const options: {method: string; body?: string} = {method};
    if (typeof body !== 'undefined') {
        options.body = JSON.stringify(body);
    }
    const response = await fetch(apiURL(path), Client4.getOptions(options));

    if (!response.ok) {
        let message = response.statusText;
        try {
            const data = await response.json();
            message = data.error || message;
        } catch {
            // Not JSON
        }
        throw new ClientError(message, response.status);
    }

    return response.json();
}

function query(params: Record<string, string | number | boolean | undefined>) {
    const search = new URLSearchParams();
    for (const [key, value] of Object.entries(params)) {
        if (typeof value !== 'undefined') {
            search.set(key, String(value));
        }
    }
    return search.toString();
}

export function getConfig() {
    return request<ClientConfig>('GET', '/config');
}

export async function getAccount() {
    return (await request<{account: Account | null}>('GET', '/account')).account;
}

export async function saveAccount(account: AccountRequest) {
    return (await request<{account: Account}>('PUT', '/account', account)).account;
}

// startOAuth starts signing in with a provider, returning the provider's page to open.
export async function startOAuth(provider: string) {
    return (await request<{url: string}>('POST', `/oauth/${encodeURIComponent(provider)}/start`)).url;
}

export function deleteAccount() {
    return request<unknown>('DELETE', '/account');
}

export function getFolders() {
    return request<Folder[]>('GET', '/folders');
}

export function getMessages(folder: string, page: number, perPage: number) {
    return request<MessagePage>('GET', `/messages?${query({folder, page, per_page: perPage})}`);
}

export function getMessage(folder: string, uid: number, options: {remoteImages?: boolean; markRead?: boolean} = {}) {
    return request<FullMessage>('GET', `/message?${query({folder, uid, remote_images: options.remoteImages, mark_read: options.markRead})}`);
}

export function attachmentURL(folder: string, uid: number, id: string) {
    return apiURL(`/attachment?${query({folder, uid, id})}`);
}

export function setFlag(folder: string, uids: number[], flag: 'seen' | 'flagged', value: boolean) {
    return request<unknown>('POST', '/messages/flag', {folder, uids, flag, value});
}

export function moveMessages(folder: string, uids: number[], destination: string) {
    return request<unknown>('POST', '/messages/move', {folder, uids, destination});
}

export function deleteMessages(folder: string, uids: number[]) {
    return request<unknown>('POST', '/messages/delete', {folder, uids});
}

export function sendMessage(message: OutgoingMessage) {
    return request<{saved_copy: boolean}>('POST', '/send', message);
}

export function saveDraft(message: OutgoingMessage) {
    return request<SavedDraft>('PUT', '/draft', message);
}

export function deleteDraft(messageId: string) {
    return request<unknown>('DELETE', `/draft?${query({message_id: messageId})}`);
}

export async function getUnread(refresh = false) {
    return (await request<{unread: number}>('GET', `/unread${refresh ? '?refresh=true' : ''}`)).unread;
}

export function shareMessage(folder: string, uid: number, channelId: string, comment: string) {
    return request<{post_id: string}>('POST', '/share', {folder, uid, channel_id: channelId, comment});
}
