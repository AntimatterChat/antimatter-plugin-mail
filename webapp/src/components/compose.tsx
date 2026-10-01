// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import React, {useRef, useState} from 'react';
import {useIntl} from 'react-intl';

import {am, own} from '../class_names';
import * as client from '../client';
import {parseAddresses, type Draft} from '../utils';

import Icon from './icon';
import {messages} from './messages';

type Props = {
    draft: Draft;
    onSent: () => void;
    onCancel: () => void;
};

// Compose writes a new message, a reply or a forward.
export default function Compose({draft: initial, onSent, onCancel}: Props) {
    const {formatMessage} = useIntl();
    const [draft, setDraft] = useState(initial);
    const [showCopies, setShowCopies] = useState(Boolean(initial.cc || initial.bcc));
    const [sending, setSending] = useState(false);
    const [error, setError] = useState('');
    const caretPlaced = useRef(false);

    const set = (field: keyof Draft) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => setDraft({...draft, [field]: e.target.value});

    const send = async (e: React.FormEvent) => {
        e.preventDefault();
        const to = parseAddresses(draft.to);
        const cc = parseAddresses(draft.cc);
        const bcc = parseAddresses(draft.bcc);
        if (to.length + cc.length + bcc.length === 0) {
            setError(formatMessage(messages.noRecipients));
            return;
        }
        setSending(true);
        setError('');
        try {
            await client.sendMessage({
                to,
                cc,
                bcc,
                subject: draft.subject,
                text: draft.text,
                in_reply_to: draft.inReplyTo,
                references: draft.references,
                reply_to_message: draft.replyTo,
            });
            onSent();
        } catch (err) {
            setError((err as Error).message);
            setSending(false);
        }
    };

    const cancel = () => {
        const changed = draft.to !== initial.to || draft.subject !== initial.subject || draft.text !== initial.text;

        // eslint-disable-next-line no-alert
        if (!changed || window.confirm(formatMessage(messages.discardConfirm))) {
            onCancel();
        }
    };

    const addressField = (field: 'to' | 'cc' | 'bcc', label: string, autoFocus = false) => (
        <label
            className={am('field')}
            htmlFor={`amm-compose-${field}`}
        >
            <span>{label}</span>
            <input
                id={`amm-compose-${field}`}
                type='text'
                inputMode='email'
                autoComplete='email'
                spellCheck={false}
                autoFocus={autoFocus}
                value={draft[field]}
                onChange={set(field)}
            />
        </label>
    );

    return (
        <form
            className={am('app-panel')}
            onSubmit={send}
        >
            <div className={own('toolbar')}>
                <button
                    type='button'
                    className={am('icon-btn')}
                    title={formatMessage(messages.discard)}
                    aria-label={formatMessage(messages.discard)}
                    onClick={cancel}
                >
                    <Icon name='back'/>
                </button>
                <span className={am('grow')}/>
                {!showCopies && (
                    <button
                        type='button'
                        className={am('chip-btn')}
                        onClick={() => setShowCopies(true)}
                    >
                        {formatMessage(messages.showCcBcc)}
                    </button>
                )}
            </div>
            {addressField('to', formatMessage(messages.to), !initial.to)}
            {showCopies && addressField('cc', formatMessage(messages.cc))}
            {showCopies && addressField('bcc', formatMessage(messages.bcc))}
            <label
                className={am('field')}
                htmlFor='amm-compose-subject'
            >
                <span>{formatMessage(messages.subject)}</span>
                <input
                    id='amm-compose-subject'
                    type='text'
                    value={draft.subject}
                    onChange={set('subject')}
                />
            </label>
            <label
                className={am('field')}
                htmlFor='amm-compose-text'
            >
                <textarea
                    id='amm-compose-text'
                    className={own('body')}
                    value={draft.text}
                    autoFocus={Boolean(initial.to)}
                    onChange={set('text')}
                    onFocus={(e) => {
                        // Replies start above the quote
                        if (!caretPlaced.current && initial.text.startsWith('\n')) {
                            e.target.setSelectionRange(0, 0);
                        }
                        caretPlaced.current = true;
                    }}
                />
            </label>
            {error && <div className={own('error')}>{error}</div>}
            <div className={own('actions')}>
                <button
                    type='submit'
                    className={am('btn', 'primary')}
                    disabled={sending}
                >
                    <Icon
                        name='send'
                        size='sm'
                    />
                    {formatMessage(sending ? messages.sending : messages.send)}
                </button>
                <button
                    type='button'
                    className={am('btn')}
                    onClick={cancel}
                >
                    {formatMessage(messages.discard)}
                </button>
            </div>
        </form>
    );
}
