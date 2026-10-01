// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import React, {useCallback, useEffect, useRef, useState} from 'react';
import {useIntl} from 'react-intl';

import {am, own} from '../class_names';
import * as client from '../client';
import type {OutgoingMessage} from '../client';
import {draftContent, parseAddresses, type Draft} from '../utils';

import Icon from './icon';
import {messages} from './messages';

// AUTOSAVE_DELAY is how long after the last change the draft is saved.
const AUTOSAVE_DELAY = 3000;

type Props = {
    draft: Draft;
    onSent: () => void;

    // Called when the message is closed without sending it: saved as a draft, or discarded
    onClose: () => void;
};

type SaveStatus = {state: 'none'} | {state: 'saving'} | {state: 'saved'; at: number} | {state: 'failed'; error: string};

function outgoing(draft: Draft): OutgoingMessage {
    return {
        to: parseAddresses(draft.to),
        cc: parseAddresses(draft.cc),
        bcc: parseAddresses(draft.bcc),
        subject: draft.subject,
        text: draft.text,
        in_reply_to: draft.inReplyTo,
        references: draft.references,
        reply_to_message: draft.replyTo,
        message_id: draft.messageId,
    };
}

// Compose writes a new message, a reply or a forward. The message is saved to the drafts folder
// a few seconds after each change, and when it's closed without sending it.
export default function Compose({draft: initial, onSent, onClose}: Props) {
    const {formatMessage, locale} = useIntl();
    const [draft, setDraft] = useState(initial);
    const [showCopies, setShowCopies] = useState(Boolean(initial.cc || initial.bcc));
    const [sending, setSending] = useState(false);
    const [error, setError] = useState('');
    const [saveStatus, setSaveStatus] = useState<SaveStatus>({state: 'none'});
    const caretPlaced = useRef(false);

    // The content of the last saved version: a message is saved once it differs from what it
    // started with (a reply starts with its quote)
    const savedContent = useRef(draftContent(initial));
    const messageId = useRef(initial.messageId);

    // Saves run one after the other; none starts once the message is sent or closed
    const saving = useRef<Promise<boolean>>(Promise.resolve(true));
    const done = useRef(false);

    const save = useCallback((toSave: Draft, force = false) => {
        saving.current = saving.current.then(async () => {
            const content = draftContent(toSave);
            if ((done.current && !force) || content === savedContent.current) {
                return true;
            }
            setSaveStatus({state: 'saving'});
            try {
                const saved = await client.saveDraft({...outgoing(toSave), message_id: messageId.current});
                messageId.current = saved.message_id;
                savedContent.current = content;
                setSaveStatus({state: 'saved', at: Date.now()});
                return true;
            } catch (err) {
                setSaveStatus({state: 'failed', error: (err as Error).message});
                return false;
            }
        });
        return saving.current;
    }, []);

    useEffect(() => {
        let timer = 0;
        if (draftContent(draft) !== savedContent.current) {
            timer = window.setTimeout(() => save(draft), AUTOSAVE_DELAY);
        }
        return () => window.clearTimeout(timer);
    }, [draft, save]);

    const set = (field: keyof Draft) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => setDraft({...draft, [field]: e.target.value});

    const send = async (e: React.FormEvent) => {
        e.preventDefault();
        const message = outgoing(draft);
        if (message.to.length + message.cc.length + message.bcc.length === 0) {
            setError(formatMessage(messages.noRecipients));
            return;
        }
        setSending(true);
        setError('');
        done.current = true;
        try {
            await saving.current;
            await client.sendMessage({...message, message_id: messageId.current});
            onSent();
        } catch (err) {
            done.current = false;
            setError((err as Error).message);
            setSending(false);
        }
    };

    // close saves the message as a draft if it changed, and closes it
    const close = async () => {
        done.current = true;
        if (await save(draft, true)) {
            onClose();
        } else {
            done.current = false;
        }
    };

    const discard = async () => {
        const changed = draftContent(draft) !== draftContent(initial);

        // eslint-disable-next-line no-alert
        if ((changed || messageId.current) && !window.confirm(formatMessage(messages.discardConfirm))) {
            return;
        }
        done.current = true;
        await saving.current;
        if (messageId.current) {
            try {
                await client.deleteDraft(messageId.current);
            } catch (err) {
                done.current = false;
                setError((err as Error).message);
                return;
            }
        }
        onClose();
    };

    let status = '';
    switch (saveStatus.state) {
    case 'saving':
        status = formatMessage(messages.draftSaving);
        break;
    case 'saved':
        status = formatMessage(messages.draftSaved, {time: new Date(saveStatus.at).toLocaleTimeString(locale, {hour: '2-digit', minute: '2-digit'})});
        break;
    case 'failed':
        status = formatMessage(messages.draftSaveFailed, {error: saveStatus.error});
        break;
    }

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
                    title={formatMessage(messages.saveAndClose)}
                    aria-label={formatMessage(messages.saveAndClose)}
                    disabled={sending}
                    onClick={close}
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
            {status && (
                <div
                    className={own('note', {failed: saveStatus.state === 'failed'})}
                    role='status'
                >
                    {status}
                </div>
            )}
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
                    disabled={sending}
                    onClick={() => save(draft, true)}
                >
                    {formatMessage(messages.saveDraft)}
                </button>
                <button
                    type='button'
                    className={am('btn')}
                    disabled={sending}
                    onClick={discard}
                >
                    {formatMessage(messages.discard)}
                </button>
            </div>
        </form>
    );
}
