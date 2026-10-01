// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import React, {useCallback, useEffect, useRef, useState} from 'react';
import {useIntl} from 'react-intl';

import {am, own} from '../class_names';
import * as client from '../client';
import type {Folder, FullMessage} from '../client';
import {displayName, formatAddresses, formatSize, fullDate, hue, initials} from '../utils';

import Icon from './icon';
import {messages} from './messages';

type Props = {
    folder: string;
    uid: number;
    folders: Folder[];
    onBack: () => void;
    onReply: (msg: FullMessage, all: boolean) => void;
    onForward: (msg: FullMessage) => void;

    // Called after the message was moved, deleted, or its read state changed
    onChanged: () => void;
};

// frameDocument wraps the sanitised HTML of a message for a sandboxed frame. The frame runs no
// script; its content security policy only lets it load the message's embedded images, and remote
// images when the user asked for them.
function frameDocument(html: string, remoteImages: boolean): string {
    const imgSrc = remoteImages ? 'data: https: http:' : 'data:';
    return '<!DOCTYPE html><html><head><meta charset="utf-8">' +
        `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src ${imgSrc}; style-src 'unsafe-inline'; font-src data:">` +
        '<base target="_blank">' +
        '<style>html,body{margin:0;padding:0;background:#fff;color:#1b1b1f;}body{padding:14px 16px;font:14px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif;overflow-wrap:anywhere;}' +
        'img{max-width:100%;height:auto;}table{max-width:100%;}pre{white-space:pre-wrap;}a{color:#1c58d9;}blockquote{margin:0 0 0 8px;padding-left:10px;border-left:3px solid #ccc;color:#555;}</style>' +
        `</head><body>${html}</body></html>`;
}

function MessageFrame({html, remoteImages, title}: {html: string; remoteImages: boolean; title: string}) {
    const ref = useRef<HTMLIFrameElement>(null);
    const observer = useRef<ResizeObserver | null>(null);
    const [height, setHeight] = useState(120);

    useEffect(() => () => observer.current?.disconnect(), []);

    // The frame grows with its content: it's same-origin without scripts, so its size can be read
    const onLoad = useCallback(() => {
        const doc = ref.current?.contentDocument;
        observer.current?.disconnect();
        if (!doc?.body) {
            return;
        }
        const measure = () => setHeight(Math.max(60, doc.documentElement.scrollHeight));
        measure();
        observer.current = new ResizeObserver(measure);
        observer.current.observe(doc.body);
        for (const img of Array.from(doc.images)) {
            img.addEventListener('load', measure);
        }
    }, []);

    return (
        <iframe
            ref={ref}
            className={own('frame')}
            title={title}
            sandbox='allow-same-origin allow-popups allow-popups-to-escape-sandbox'
            referrerPolicy='no-referrer'
            srcDoc={frameDocument(html, remoteImages)}
            style={{height}}
            onLoad={onLoad}
        />
    );
}

// MessageView is the reading view of a message.
export default function MessageView({folder, uid, folders, onBack, onReply, onForward, onChanged}: Props) {
    const {formatMessage, locale} = useIntl();
    const [msg, setMsg] = useState<FullMessage | null>(null);
    const [remoteImages, setRemoteImages] = useState(false);
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);

    const load = useCallback(async (withRemoteImages: boolean) => {
        setError('');
        try {
            const result = await client.getMessage(folder, uid, {remoteImages: withRemoteImages, markRead: true});
            setMsg(result);
            setRemoteImages(withRemoteImages);
        } catch (err) {
            setError((err as Error).message);
        }
    }, [folder, uid]);

    useEffect(() => {
        setMsg(null);

        // Opening the message marks it as read
        load(false).then(onChanged);
    }, [load]);

    const run = async (action: () => Promise<unknown>, after: () => void) => {
        setBusy(true);
        setError('');
        try {
            await action();
            onChanged();
            after();
        } catch (err) {
            setError((err as Error).message);
        } finally {
            setBusy(false);
        }
    };

    const current = folders.find((f) => f.name === folder);
    const deleteMessage = () => {
        // eslint-disable-next-line no-alert
        if (current?.role === 'trash' && !window.confirm(formatMessage(messages.deleteForever))) {
            return;
        }
        run(() => client.deleteMessages(folder, [uid]), onBack);
    };

    const sender = msg?.from[0];
    const senderName = displayName(sender) || formatMessage(messages.unknownSender);

    return (
        <div className={am('app-panel')}>
            <div className={own('toolbar')}>
                <button
                    className={am('icon-btn')}
                    title={formatMessage(messages.back)}
                    aria-label={formatMessage(messages.back)}
                    onClick={onBack}
                >
                    <Icon name='back'/>
                </button>
                <span className={am('grow')}/>
                {msg && (
                    <>
                        <button
                            className={am('icon-btn')}
                            title={formatMessage(messages.reply)}
                            aria-label={formatMessage(messages.reply)}
                            onClick={() => onReply(msg, false)}
                        >
                            <Icon name='reply'/>
                        </button>
                        <button
                            className={am('icon-btn')}
                            title={formatMessage(messages.replyAll)}
                            aria-label={formatMessage(messages.replyAll)}
                            onClick={() => onReply(msg, true)}
                        >
                            <Icon name='undo'/>
                        </button>
                        <button
                            className={am('icon-btn')}
                            title={formatMessage(messages.forward)}
                            aria-label={formatMessage(messages.forward)}
                            onClick={() => onForward(msg)}
                        >
                            <Icon name='forward'/>
                        </button>
                        <button
                            className={am('icon-btn')}
                            title={formatMessage(messages.markUnread)}
                            aria-label={formatMessage(messages.markUnread)}
                            disabled={busy}
                            onClick={() => run(() => client.setFlag(folder, [uid], 'seen', false), onBack)}
                        >
                            <Icon name='mail'/>
                        </button>
                        <button
                            className={am('icon-btn')}
                            title={formatMessage(messages.delete)}
                            aria-label={formatMessage(messages.delete)}
                            disabled={busy}
                            onClick={deleteMessage}
                        >
                            <Icon name='trash'/>
                        </button>
                    </>
                )}
            </div>
            {error && <div className={own('error')}>{error}</div>}
            {!msg && !error && <div className={am('empty')}>{formatMessage(messages.loading)}</div>}
            {msg && (
                <>
                    <h3 className={own('subject')}>{msg.subject || formatMessage(messages.noSubject)}</h3>
                    <div className={own('header')}>
                        <span
                            className={am('av')}
                            style={{'--am-h': hue(senderName), '--amm-h': hue(senderName)} as React.CSSProperties}
                            aria-hidden='true'
                        >
                            {initials(senderName)}
                        </span>
                        <div className={own('header-text')}>
                            <b title={sender?.email}>{senderName}</b>
                            {sender?.name && <span className={am('sub')}>{` <${sender.email}>`}</span>}
                            <div className={am('sub')}>
                                {formatMessage(messages.to)}{': '}{formatAddresses(msg.to)}
                                {msg.cc.length > 0 && <>{' · '}{formatMessage(messages.cc)}{': '}{formatAddresses(msg.cc)}</>}
                            </div>
                            {msg.date > 0 && <div className={am('sub')}>{fullDate(msg.date, locale)}</div>}
                        </div>
                    </div>
                    <div className={own('actions')}>
                        <select
                            className={own('folder-select')}
                            aria-label={formatMessage(messages.moveTo)}
                            value=''
                            disabled={busy}
                            onChange={(e) => {
                                const destination = e.target.value;
                                if (destination) {
                                    run(() => client.moveMessages(folder, [uid], destination), onBack);
                                }
                            }}
                        >
                            <option value=''>{formatMessage(messages.moveTo)}</option>
                            {folders.filter((f) => f.name !== folder).map((f) => (
                                <option
                                    key={f.name}
                                    value={f.name}
                                >
                                    {f.name}
                                </option>
                            ))}
                        </select>
                    </div>
                    {msg.remote_images > 0 && !remoteImages && (
                        <div className={own('notice')}>
                            {formatMessage(messages.remoteImagesBlocked, {count: msg.remote_images})}
                            {' '}
                            <button
                                className={own('link')}
                                onClick={() => load(true)}
                            >
                                {formatMessage(messages.remoteImagesShow)}
                            </button>
                        </div>
                    )}
                    {msg.html ? (
                        <MessageFrame
                            html={msg.html}
                            remoteImages={remoteImages}
                            title={msg.subject}
                        />
                    ) : (
                        <div className={own('text')}>{msg.text}</div>
                    )}
                    {msg.attachments.length > 0 && (
                        <>
                            <div className={am('app-h')}>
                                <span className={am('grow')}>{formatMessage(messages.attachments)}</span>
                            </div>
                            {msg.attachments.map((a) => (
                                <a
                                    key={a.id}
                                    className={am('app-row')}
                                    href={client.attachmentURL(folder, uid, a.id)}
                                    download={a.filename}
                                    aria-label={formatMessage(messages.download, {filename: a.filename})}
                                >
                                    <Icon name='attach'/>
                                    <span className={own('row-main')}>
                                        <b>{a.filename}</b>
                                        <span className={am('sub')}>{a.content_type}</span>
                                    </span>
                                    <span className={am('sub')}>{formatSize(a.size)}</span>
                                </a>
                            ))}
                        </>
                    )}
                </>
            )}
        </div>
    );
}
