// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import type {FullMessage} from './client';
import {draftContent, draftFromMessage, forwardDraft, initials, listDate, parseAddresses, quote, replyDraft} from './utils';

const message: FullMessage = {
    folder: 'INBOX',
    uid: 7,
    message_id: 'draft-2@example.com',
    in_reply_to: [],
    references: ['draft-1@example.com'],
    from: [{name: 'Marie Laurent', email: 'marie@example.com'}],
    to: [{name: 'Ada', email: 'ada@example.com'}, {email: 'lena@example.com'}],
    cc: [{email: 'kenji@example.com'}],
    bcc: [],
    reply_to: [],
    subject: 'Beam schedule',
    date: 0,
    unread: false,
    flagged: false,
    draft: false,
    text: 'Wed or Fri?\r\n\r\nMarie',
    remote_images: 0,
    attachments: [],
};

describe('parseAddresses', () => {
    test('splits on commas and semicolons, outside quotes and angle brackets', () => {
        expect(parseAddresses('a@example.com, "Laurent, Marie" <marie@example.com>; b@example.com,')).toEqual([
            {email: 'a@example.com'},
            {email: '"Laurent, Marie" <marie@example.com>'},
            {email: 'b@example.com'},
        ]);
        expect(parseAddresses('  ')).toEqual([]);
    });
});

describe('initials', () => {
    test('uses the first and last words', () => {
        expect(initials('Marie Laurent')).toBe('ML');
        expect(initials('Journal of Applied Physics')).toBe('JP');
        expect(initials('supplier@example.com')).toBe('SU');
        expect(initials('')).toBe('?');
    });
});

describe('listDate', () => {
    const now = new Date(2026, 9, 1, 15, 0);
    test('shows the time today, yesterday, the weekday this week, the date before', () => {
        expect(listDate(new Date(2026, 9, 1, 11, 52).getTime(), now).kind).toBe('time');
        expect(listDate(new Date(2026, 8, 30, 23, 0).getTime(), now).kind).toBe('yesterday');
        expect(listDate(new Date(2026, 8, 26, 9, 0).getTime(), now).kind).toBe('weekday');
        expect(listDate(new Date(2026, 8, 20, 9, 0).getTime(), now).kind).toBe('date');
    });
});

describe('drafts', () => {
    test('reply to the sender', () => {
        const draft = replyDraft(message, 'ada@example.com', false, 'Marie wrote:');
        expect(draft.to).toBe('Marie Laurent <marie@example.com>');
        expect(draft.cc).toBe('');
        expect(draft.subject).toBe('Re: Beam schedule');
        expect(draft.text).toBe('\n\nMarie wrote:\n> Wed or Fri?\n>\n> Marie\n');
        expect(draft.inReplyTo).toBe('draft-2@example.com');
        expect(draft.references).toEqual(['draft-1@example.com', 'draft-2@example.com']);
        expect(draft.replyTo).toEqual({folder: 'INBOX', uid: 7});
    });

    test('reply to everyone but me', () => {
        const draft = replyDraft({...message, subject: 'RE: Beam schedule'}, 'ADA@example.com', true, '');
        expect(draft.to).toBe('Marie Laurent <marie@example.com>');
        expect(draft.cc).toBe('lena@example.com, kenji@example.com');
        expect(draft.subject).toBe('RE: Beam schedule');
    });

    test('forward', () => {
        const draft = forwardDraft(message, '--- Forwarded ---');
        expect(draft.to).toBe('');
        expect(draft.subject).toBe('Fwd: Beam schedule');
        expect(draft.text).toBe('\n\n--- Forwarded ---\n\nWed or Fri?\n\nMarie\n');
    });

    test('quote', () => {
        expect(quote('a\n\nb\n')).toBe('> a\n>\n> b');
    });
});

describe('saved drafts', () => {
    test('reopen with their recipients, thread and Message-ID', () => {
        const draft = draftFromMessage({
            ...message,
            draft: true,
            in_reply_to: ['draft-1@example.com'],
            bcc: [{email: 'boss@example.com'}],
        });
        expect(draft).toEqual({
            to: 'Ada <ada@example.com>, lena@example.com',
            cc: 'kenji@example.com',
            bcc: 'boss@example.com',
            subject: 'Beam schedule',
            text: 'Wed or Fri?\n\nMarie',
            inReplyTo: 'draft-1@example.com',
            references: ['draft-1@example.com'],
            messageId: 'draft-2@example.com',
        });
    });

    test('change when what they say changes', () => {
        const draft = draftFromMessage(message);
        expect(draftContent({...draft, messageId: 'other@example.com'})).toBe(draftContent(draft));
        expect(draftContent({...draft, bcc: 'boss@example.com'})).not.toBe(draftContent(draft));
    });
});
