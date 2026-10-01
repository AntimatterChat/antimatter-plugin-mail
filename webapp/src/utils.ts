// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import type {Address, FullMessage} from './client';

// displayName returns the name to show for an address.
export function displayName(address?: Address): string {
    if (!address) {
        return '';
    }
    return address.name || address.email;
}

// formatAddress formats an address as in a message header.
export function formatAddress(address: Address): string {
    return address.name ? `${address.name} <${address.email}>` : address.email;
}

export function formatAddresses(addresses: Address[]): string {
    return addresses.map(formatAddress).join(', ');
}

// parseAddresses splits a list of addresses typed by the user. The server checks each address.
export function parseAddresses(text: string): Address[] {
    const addresses: Address[] = [];
    let current = '';
    let quoted = false;
    let angle = false;
    for (const c of text) {
        if (c === '"') {
            quoted = !quoted;
        } else if (c === '<') {
            angle = true;
        } else if (c === '>') {
            angle = false;
        }
        if ((c === ',' || c === ';' || c === '\n') && !quoted && !angle) {
            if (current.trim()) {
                addresses.push({email: current.trim()});
            }
            current = '';
            continue;
        }
        current += c;
    }
    if (current.trim()) {
        addresses.push({email: current.trim()});
    }
    return addresses;
}

export function initials(name: string): string {
    // Bare addresses use their local part
    const base = name.includes('@') && !name.trim().includes(' ') ? name.slice(0, name.indexOf('@')) : name;
    const words = base.replace(/[<>"@.]/g, ' ').trim().split(/\s+/).filter(Boolean);
    if (words.length === 0) {
        return '?';
    }
    if (words.length === 1) {
        return words[0].slice(0, 2).toUpperCase();
    }
    return (words[0][0] + words[words.length - 1][0]).toUpperCase();
}

// hue returns the avatar hue of a name, as in the Fusion mockup.
export function hue(name: string): number {
    return (name.length * 47) % 360;
}

function sameDay(a: Date, b: Date) {
    return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
}

export type DateLabel = {kind: 'time' | 'yesterday' | 'weekday' | 'date'; text: string};

// listDate returns how a message list shows a date: the time today, "yesterday", the day of the
// week this week, and the date before.
export function listDate(timestamp: number, now = new Date(), locale?: string): DateLabel {
    const date = new Date(timestamp);
    if (sameDay(date, now)) {
        return {kind: 'time', text: date.toLocaleTimeString(locale, {hour: '2-digit', minute: '2-digit'})};
    }
    const yesterday = new Date(now);
    yesterday.setDate(now.getDate() - 1);
    if (sameDay(date, yesterday)) {
        return {kind: 'yesterday', text: ''};
    }
    const weekAgo = new Date(now);
    weekAgo.setDate(now.getDate() - 6);
    weekAgo.setHours(0, 0, 0, 0);
    if (date >= weekAgo && date <= now) {
        return {kind: 'weekday', text: date.toLocaleDateString(locale, {weekday: 'short'})};
    }
    const options: Intl.DateTimeFormatOptions = {day: 'numeric', month: 'short'};
    if (date.getFullYear() !== now.getFullYear()) {
        options.year = 'numeric';
    }
    return {kind: 'date', text: date.toLocaleDateString(locale, options)};
}

export function fullDate(timestamp: number, locale?: string): string {
    return new Date(timestamp).toLocaleString(locale, {dateStyle: 'medium', timeStyle: 'short'});
}

export function formatSize(bytes: number): string {
    if (bytes < 1024) {
        return `${bytes} B`;
    }
    if (bytes < 1024 * 1024) {
        return `${Math.round(bytes / 1024)} KB`;
    }
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function prefixed(subject: string, prefix: string): string {
    const re = new RegExp(`^${prefix}\\s*:`, 'i');
    return re.test(subject.trim()) ? subject.trim() : `${prefix}: ${subject.trim()}`;
}

export function quote(text: string): string {
    return text.replace(/\r\n/g, '\n').trimEnd().split('\n').map((line) => (line ? `> ${line}` : '>')).join('\n');
}

export type Draft = {
    to: string;
    cc: string;
    bcc: string;
    subject: string;
    text: string;
    inReplyTo?: string;
    references?: string[];
    replyTo?: {folder: string; uid: number};

    // The Message-ID of the saved draft, once saved
    messageId?: string;
};

export const emptyDraft: Draft = {to: '', cc: '', bcc: '', subject: '', text: ''};

function sameAddress(a: Address, b: string) {
    return a.email.toLowerCase() === b.toLowerCase();
}

// replyDraft starts a reply to a message, to its sender or to everyone. headerLine is the
// translated "On {date}, {name} wrote:" line.
export function replyDraft(msg: FullMessage, me: string, all: boolean, headerLine: string): Draft {
    const to = msg.reply_to.length > 0 ? msg.reply_to : msg.from;
    let cc: Address[] = [];
    if (all) {
        cc = [...msg.to, ...msg.cc].filter((a) => !sameAddress(a, me) && !to.some((t) => t.email.toLowerCase() === a.email.toLowerCase()));
    }
    const references = [...msg.references];
    if (msg.message_id && !references.includes(msg.message_id)) {
        references.push(msg.message_id);
    }
    return {
        to: formatAddresses(to.filter((a) => !all || !sameAddress(a, me) || to.length === 1)),
        cc: formatAddresses(cc),
        bcc: '',
        subject: prefixed(msg.subject, 'Re'),
        text: `\n\n${headerLine}\n${quote(msg.text)}\n`,
        ...(msg.message_id ? {inReplyTo: msg.message_id} : {}),
        references,
        replyTo: {folder: msg.folder, uid: msg.uid},
    };
}

// forwardDraft starts forwarding a message. header is the translated block naming the original
// sender, date, subject and recipients.
export function forwardDraft(msg: FullMessage, header: string): Draft {
    return {
        ...emptyDraft,
        subject: prefixed(msg.subject, 'Fwd'),
        text: `\n\n${header}\n\n${msg.text.replace(/\r\n/g, '\n').trimEnd()}\n`,
    };
}

// draftFromMessage reopens a draft saved in the drafts folder.
export function draftFromMessage(msg: FullMessage): Draft {
    return {
        to: formatAddresses(msg.to),
        cc: formatAddresses(msg.cc),
        bcc: formatAddresses(msg.bcc),
        subject: msg.subject,
        text: msg.text.replace(/\r\n/g, '\n'),
        ...(msg.in_reply_to.length > 0 ? {inReplyTo: msg.in_reply_to[0]} : {}),
        references: msg.references,
        ...(msg.message_id ? {messageId: msg.message_id} : {}),
    };
}

// draftContent returns what a draft says, to tell whether it changed since it was saved.
export function draftContent(draft: Draft): string {
    return JSON.stringify([draft.to, draft.cc, draft.bcc, draft.subject, draft.text]);
}
