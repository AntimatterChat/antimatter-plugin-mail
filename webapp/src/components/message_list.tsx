// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import React, {useCallback, useEffect, useState} from 'react';
import {useIntl} from 'react-intl';

import {am, own} from '../class_names';
import * as client from '../client';
import type {Folder, MessageSummary} from '../client';
import {displayName, hue, initials, listDate} from '../utils';

import Icon from './icon';
import {messages} from './messages';

const PER_PAGE = 50;

type Props = {
    folder: string;
    folders: Folder[];

    // Changes when the list must be loaded again
    reloadKey: number;
    onFolderChange: (folder: string) => void;
    onOpen: (uid: number) => void;
    onCompose: () => void;
    onSettings: () => void;
    onRefresh: () => void;
};

export function useFolderName() {
    const {formatMessage} = useIntl();
    return useCallback((folder: Folder) => {
        switch (folder.role) {
        case 'inbox': return formatMessage(messages.folderInbox);
        case 'drafts': return formatMessage(messages.folderDrafts);
        case 'sent': return formatMessage(messages.folderSent);
        case 'archive': return formatMessage(messages.folderArchive);
        case 'junk': return formatMessage(messages.folderJunk);
        case 'trash': return formatMessage(messages.folderTrash);
        default: return folder.name;
        }
    }, [formatMessage]);
}

function MessageRow({message, sentFolder, onOpen}: {message: MessageSummary; sentFolder: boolean; onOpen: (uid: number) => void}) {
    const {formatMessage, locale} = useIntl();

    // The sent folder shows who messages went to
    const who = sentFolder ? message.to.map(displayName).join(', ') : displayName(message.from[0]);
    const name = who || formatMessage(messages.unknownSender);
    const date = listDate(message.date, new Date(), locale);
    const subject = message.subject || formatMessage(messages.noSubject);

    return (
        <button
            className={am('app-row', {unread: message.unread})}
            onClick={() => onOpen(message.uid)}
        >
            <span
                className={am('av', 'sm')}
                style={{'--am-h': hue(name), '--amm-h': hue(name)} as React.CSSProperties}
                aria-hidden='true'
            >
                {initials(name)}
            </span>
            <span className={own('row-main')}>
                <b>{`${name} — ${subject}`}</b>
                <span className={am('sub')}>{message.snippet}</span>
            </span>
            <span className={am('sub')}>
                {message.has_attachments && (
                    <span title={formatMessage(messages.hasAttachments)}>
                        <Icon
                            name='attach'
                            size='xs'
                        />
                    </span>
                )}
                {message.unread && (
                    <span
                        className={am('dot-u')}
                        style={{display: 'inline-block', marginRight: 6}}
                        title={formatMessage(messages.unreadMessage)}
                    />
                )}
                {date.kind === 'yesterday' ? formatMessage(messages.yesterday) : date.text}
            </span>
        </button>
    );
}

// appendPage adds a page of older messages to a list. New mail shifts the pages, so the messages
// already shown are skipped.
function appendPage(list: MessageSummary[], page: MessageSummary[]) {
    const shown = new Set(list.map((m) => m.uid));
    return [...list, ...page.filter((m) => !shown.has(m.uid))];
}

// MessageList shows the messages of a folder, newest first.
export default function MessageList({folder, folders, reloadKey, onFolderChange, onOpen, onCompose, onSettings, onRefresh}: Props) {
    const {formatMessage} = useIntl();
    const folderName = useFolderName();
    const [list, setList] = useState<MessageSummary[] | null>(null);
    const [total, setTotal] = useState(0);
    const [page, setPage] = useState(0);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState('');

    const load = useCallback(async (pageToLoad: number) => {
        setLoading(true);
        setError('');
        try {
            const result = await client.getMessages(folder, pageToLoad, PER_PAGE);
            setList((current) => (pageToLoad === 0 || !current ? result.messages : appendPage(current, result.messages)));
            setTotal(result.total);
            setPage(pageToLoad);
        } catch (err) {
            setError((err as Error).message);
        } finally {
            setLoading(false);
        }
    }, [folder]);

    useEffect(() => {
        setList(null);
        load(0);
    }, [load, reloadKey]);

    const current = folders.find((f) => f.name === folder);
    const sentFolder = current?.role === 'sent' || current?.role === 'drafts';

    return (
        <div className={am('app-panel')}>
            <div className={own('toolbar')}>
                <select
                    className={own('folder-select')}
                    aria-label={formatMessage(messages.folder)}
                    value={folder}
                    onChange={(e) => onFolderChange(e.target.value)}
                >
                    {folders.map((f) => (
                        <option
                            key={f.name}
                            value={f.name}
                        >
                            {f.unread > 0 ? `${folderName(f)} (${f.unread})` : folderName(f)}
                        </option>
                    ))}
                </select>
                <span className={am('grow')}/>
                <button
                    className={am('icon-btn')}
                    title={formatMessage(messages.compose)}
                    aria-label={formatMessage(messages.compose)}
                    onClick={onCompose}
                >
                    <Icon name='pen'/>
                </button>
                <button
                    className={am('icon-btn')}
                    title={formatMessage(messages.refresh)}
                    aria-label={formatMessage(messages.refresh)}
                    onClick={onRefresh}
                >
                    <Icon name='refresh'/>
                </button>
                <button
                    className={am('icon-btn')}
                    title={formatMessage(messages.settings)}
                    aria-label={formatMessage(messages.settings)}
                    onClick={onSettings}
                >
                    <Icon name='cog'/>
                </button>
            </div>
            <div className={am('app-h')}>
                <span className={am('grow')}>{current ? folderName(current) : folder}</span>
                {Boolean(current?.unread) && <span>{formatMessage(messages.unreadCount, {count: current?.unread})}</span>}
            </div>
            {error && (
                <div className={own('error')}>
                    {error}
                    {' '}
                    <button
                        className={own('link')}
                        onClick={() => load(page)}
                    >
                        {formatMessage(messages.retry)}
                    </button>
                </div>
            )}
            {list === null && !error && <div className={am('empty')}>{formatMessage(messages.loading)}</div>}
            {list && list.length === 0 && <div className={am('empty')}>{formatMessage(messages.emptyFolder)}</div>}
            {list?.map((message) => (
                <MessageRow
                    key={message.uid}
                    message={message}
                    sentFolder={sentFolder}
                    onOpen={onOpen}
                />
            ))}
            {list && list.length < total && (
                <button
                    className={am('chip-btn')}
                    disabled={loading}
                    onClick={() => load(page + 1)}
                >
                    {formatMessage(loading ? messages.loading : messages.loadMore)}
                </button>
            )}
        </div>
    );
}
