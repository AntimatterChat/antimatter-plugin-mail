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
    onOpen: (message: MessageSummary) => void;
    onCompose: () => void;
    onSettings: () => void;
    onRefresh: () => void;

    // Called after messages were marked or deleted from the list
    onChanged: () => void;
};

// MAX_UIDS is the number of messages one request acts on, as the server allows.
const MAX_UIDS = 500;

function chunks(uids: number[]) {
    const result: number[][] = [];
    for (let i = 0; i < uids.length; i += MAX_UIDS) {
        result.push(uids.slice(i, i + MAX_UIDS));
    }
    return result;
}

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

type RowProps = {
    message: MessageSummary;
    sentFolder: boolean;
    selected: boolean;
    onSelect: (uid: number, selected: boolean) => void;
    onOpen: (summary: MessageSummary) => void;
};

function MessageRow({message, sentFolder, selected, onSelect, onOpen}: RowProps) {
    const {formatMessage, locale} = useIntl();

    // The sent folder shows who messages went to
    const who = sentFolder ? message.to.map(displayName).join(', ') : displayName(message.from[0]);
    const name = who || formatMessage(messages.unknownSender);
    const date = listDate(message.date, new Date(), locale);
    const subject = message.subject || formatMessage(messages.noSubject);

    return (
        <div className={own('row-wrap', {selected})}>
            <input
                type='checkbox'
                className={own('row-check')}
                checked={selected}
                aria-label={formatMessage(messages.selectMessage, {subject})}
                onChange={(e) => onSelect(message.uid, e.target.checked)}
            />
            <button
                className={am('app-row', {unread: message.unread})}
                onClick={() => onOpen(message)}
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
        </div>
    );
}

// appendPage adds a page of older messages to a list. New mail shifts the pages, so the messages
// already shown are skipped.
function appendPage(list: MessageSummary[], page: MessageSummary[]) {
    const shown = new Set(list.map((m) => m.uid));
    return [...list, ...page.filter((m) => !shown.has(m.uid))];
}

// MessageList shows the messages of a folder, newest first.
export default function MessageList({folder, folders, reloadKey, onFolderChange, onOpen, onCompose, onSettings, onRefresh, onChanged}: Props) {
    const {formatMessage} = useIntl();
    const folderName = useFolderName();
    const [list, setList] = useState<MessageSummary[] | null>(null);
    const [total, setTotal] = useState(0);
    const [page, setPage] = useState(0);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState('');
    const [selected, setSelected] = useState<Set<number>>(new Set());
    const [busy, setBusy] = useState(false);
    const [searching, setSearching] = useState(false);
    const [searchText, setSearchText] = useState('');
    const [search, setSearch] = useState('');

    const load = useCallback(async (pageToLoad: number) => {
        setLoading(true);
        setError('');
        try {
            const result = await client.getMessages(folder, pageToLoad, PER_PAGE, search);
            setList((current) => (pageToLoad === 0 || !current ? result.messages : appendPage(current, result.messages)));
            setTotal(result.total);
            setPage(pageToLoad);
        } catch (err) {
            setError((err as Error).message);
        } finally {
            setLoading(false);
        }
    }, [folder, search]);

    // A search is in the folder it was made in
    useEffect(() => {
        setSearch('');
        setSearchText('');
        setSearching(false);
    }, [folder]);

    useEffect(() => {
        setList(null);
        setSelected(new Set());
        load(0);
    }, [load, reloadKey]);

    const current = folders.find((f) => f.name === folder);
    const sentFolder = current?.role === 'sent' || current?.role === 'drafts';

    const select = useCallback((uid: number, on: boolean) => {
        setSelected((previous) => {
            const next = new Set(previous);
            if (on) {
                next.add(uid);
            } else {
                next.delete(uid);
            }
            return next;
        });
    }, []);
    const allSelected = Boolean(list?.length) && list?.every((m) => selected.has(m.uid));

    // act runs an action on the selected messages, then updates the list
    const act = async (action: (uids: number[]) => Promise<unknown>, update: (shown: MessageSummary[], uids: Set<number>) => MessageSummary[]) => {
        const uids = Array.from(selected);
        setBusy(true);
        setError('');
        try {
            for (const chunk of chunks(uids)) {
                // eslint-disable-next-line no-await-in-loop
                await action(chunk);
            }
            const done = new Set(uids);
            setList((shown) => shown && update(shown, done));
            setSelected(new Set());
            onChanged();
            return true;
        } catch (err) {
            setError((err as Error).message);
            return false;
        } finally {
            setBusy(false);
        }
    };
    const markSelected = (read: boolean) => act(
        (uids) => client.setFlag(folder, uids, 'seen', read),
        (shown, uids) => shown.map((m) => (uids.has(m.uid) ? {...m, unread: !read} : m)),
    );
    const deleteSelected = async () => {
        const count = selected.size;

        // eslint-disable-next-line no-alert
        if (current?.role === 'trash' && !window.confirm(formatMessage(messages.deleteSelectedForever, {count}))) {
            return;
        }
        const deleted = await act(
            (uids) => client.deleteMessages(folder, uids),
            (shown, uids) => shown.filter((m) => !uids.has(m.uid)),
        );
        if (deleted) {
            setTotal((t) => Math.max(0, t - count));
        }
    };

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
                    className={am('icon-btn', {on: searching})}
                    title={formatMessage(messages.search)}
                    aria-label={formatMessage(messages.search)}
                    aria-pressed={searching}
                    onClick={() => {
                        if (searching) {
                            setSearch('');
                            setSearchText('');
                        }
                        setSearching(!searching);
                    }}
                >
                    <Icon name='search'/>
                </button>
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
            {searching && (
                <form
                    className={own('search')}
                    role='search'
                    onSubmit={(e) => {
                        e.preventDefault();
                        setSearch(searchText.trim());
                    }}
                >
                    <input
                        type='search'
                        className={own('search-input')}
                        value={searchText}
                        placeholder={formatMessage(messages.searchPlaceholder)}
                        aria-label={formatMessage(messages.searchPlaceholder)}
                        maxLength={200}
                        autoFocus={true}
                        onChange={(e) => setSearchText(e.target.value)}
                    />
                    {search && (
                        <button
                            type='button'
                            className={am('icon-btn')}
                            title={formatMessage(messages.clearSearch)}
                            aria-label={formatMessage(messages.clearSearch)}
                            onClick={() => {
                                setSearch('');
                                setSearchText('');
                            }}
                        >
                            <Icon name='x'/>
                        </button>
                    )}
                </form>
            )}
            {search && list && (
                <div className={am('sub')}>{formatMessage(messages.searchResults, {count: total, query: search})}</div>
            )}
            <div className={am('app-h')}>
                {Boolean(list?.length) && (
                    <input
                        type='checkbox'
                        className={own('row-check')}
                        checked={allSelected}
                        aria-label={formatMessage(messages.selectAll)}
                        title={formatMessage(messages.selectAll)}
                        onChange={(e) => setSelected(new Set(e.target.checked ? list?.map((m) => m.uid) : []))}
                    />
                )}
                <span className={am('grow')}>{current ? folderName(current) : folder}</span>
                {Boolean(current?.unread) && <span>{formatMessage(messages.unreadCount, {count: current?.unread})}</span>}
            </div>
            {selected.size > 0 && (
                <div className={own('actions', 'selection')}>
                    <span className={am('sub')}>{formatMessage(messages.selectedCount, {count: selected.size})}</span>
                    <span className={am('grow')}/>
                    <button
                        className={am('chip-btn')}
                        disabled={busy}
                        onClick={() => markSelected(true)}
                    >
                        {formatMessage(messages.markRead)}
                    </button>
                    <button
                        className={am('chip-btn')}
                        disabled={busy}
                        onClick={() => markSelected(false)}
                    >
                        {formatMessage(messages.markUnread)}
                    </button>
                    <button
                        className={am('icon-btn')}
                        title={formatMessage(messages.delete)}
                        aria-label={formatMessage(messages.delete)}
                        disabled={busy}
                        onClick={deleteSelected}
                    >
                        <Icon name='trash'/>
                    </button>
                    <button
                        className={am('icon-btn')}
                        title={formatMessage(messages.clearSelection)}
                        aria-label={formatMessage(messages.clearSelection)}
                        onClick={() => setSelected(new Set())}
                    >
                        <Icon name='x'/>
                    </button>
                </div>
            )}
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
            {list && list.length === 0 && !search && <div className={am('empty')}>{formatMessage(messages.emptyFolder)}</div>}
            {list?.map((message) => (
                <MessageRow
                    key={message.uid}
                    message={message}
                    sentFolder={sentFolder}
                    selected={selected.has(message.uid)}
                    onSelect={select}
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
