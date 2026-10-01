// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import React, {useCallback, useEffect, useState} from 'react';
import {useIntl} from 'react-intl';
import {useStore} from 'react-redux';

import {loadUnread, setUnread} from '../actions';
import {am, own} from '../class_names';
import * as client from '../client';
import type {Account, Folder, FullMessage, MessageSummary, OAuthProvider} from '../client';
import type {PluginStore} from '../types/host';
import {displayName, draftFromMessage, emptyDraft, forwardDraft, formatAddresses, fullDate, replyDraft, type Draft} from '../utils';
import {isFusionUI} from '../web_ui';

import AccountForm from './account_form';
import Compose from './compose';
import MessageList from './message_list';
import MessageView from './message_view';
import {messages} from './messages';

type View =
    {kind: 'list'} |
    {kind: 'message'; uid: number} |
    {kind: 'compose'; draft: Draft; back: View} |
    {kind: 'settings'};

const INBOX = 'INBOX';

// MailPanel is the mail app, shown in the right-hand panel.
export default function MailPanel() {
    const {formatMessage, locale} = useIntl();
    const store = useStore() as PluginStore;
    const [account, setAccount] = useState<Account | null>(null);
    const [loaded, setLoaded] = useState(false);
    const [allowInsecure, setAllowInsecure] = useState(false);
    const [providers, setProviders] = useState<OAuthProvider[]>([]);
    const [folders, setFolders] = useState<Folder[]>([]);
    const [folder, setFolder] = useState(INBOX);
    const [view, setView] = useState<View>({kind: 'list'});
    const [reloadKey, setReloadKey] = useState(0);
    const [notice, setNotice] = useState('');
    const [error, setError] = useState('');

    const loadAccount = useCallback(async () => {
        setError('');
        try {
            const [loadedAccount, config] = await Promise.all([client.getAccount(), client.getConfig()]);
            setAccount(loadedAccount);
            setAllowInsecure(config.allow_insecure_connections);
            setProviders(config.oauth_providers || []);
            setLoaded(true);
        } catch (err) {
            setError((err as Error).message);
        }
    }, []);

    const loadFolders = useCallback(async () => {
        try {
            setFolders(await client.getFolders());
            setError('');
        } catch (err) {
            setError((err as Error).message);
        }
    }, []);

    useEffect(() => {
        loadAccount();
    }, [loadAccount]);

    useEffect(() => {
        if (account) {
            loadFolders();
        }
    }, [account, loadFolders]);

    // After reading, moving or deleting messages: the counts changed
    const onChanged = useCallback(() => {
        loadFolders();
        loadUnread(store, true);
    }, [loadFolders, store]);

    const refresh = () => {
        setNotice('');
        onChanged();
        setReloadKey((k) => k + 1);
    };

    const backToList = () => {
        setView({kind: 'list'});
        setReloadKey((k) => k + 1);
    };

    // Drafts open in the editor
    const open = async (message: MessageSummary) => {
        setNotice('');
        const current = folders.find((f) => f.name === folder);
        if (!message.draft && current?.role !== 'drafts') {
            setView({kind: 'message', uid: message.uid});
            return;
        }
        try {
            const msg = await client.getMessage(folder, message.uid);
            setError('');
            setView({kind: 'compose', draft: draftFromMessage(msg), back: {kind: 'list'}});
        } catch (err) {
            setError((err as Error).message);
        }
    };

    const reply = (msg: FullMessage, all: boolean) => {
        const sender = msg.from[0];
        const header = formatMessage(messages.replyHeader, {date: fullDate(msg.date, locale), name: displayName(sender)});
        setView({kind: 'compose', draft: replyDraft(msg, account?.email || '', all, header), back: view});
    };

    const forward = (msg: FullMessage) => {
        const header = [
            formatMessage(messages.forwardHeader),
            `${formatMessage(messages.from)}: ${formatAddresses(msg.from)}`,
            `${formatMessage(messages.subject)}: ${msg.subject}`,
            `${formatMessage(messages.to)}: ${formatAddresses(msg.to)}`,
            msg.date ? fullDate(msg.date, locale) : '',
        ].filter(Boolean).join('\n');
        setView({kind: 'compose', draft: forwardDraft(msg, header), back: view});
    };

    let content: React.ReactNode;
    if (error && !loaded) {
        content = (
            <div className={am('empty')}>
                {error}
                {' '}
                <button
                    className={own('link')}
                    onClick={loadAccount}
                >
                    {formatMessage(messages.retry)}
                </button>
            </div>
        );
    } else if (!loaded) {
        content = <div className={am('empty')}>{formatMessage(messages.loading)}</div>;
    } else if (account === null || view.kind === 'settings') {
        content = (
            <AccountForm
                account={account}
                allowInsecure={allowInsecure}
                providers={providers}
                onSaved={(saved) => {
                    setAccount(saved);
                    setFolder(INBOX);
                    setView({kind: 'list'});
                    setReloadKey((k) => k + 1);
                    loadUnread(store, true);
                }}
                onCancel={account ? () => setView({kind: 'list'}) : null}
                onDisconnected={() => {
                    setAccount(null);
                    setFolders([]);
                    setView({kind: 'list'});
                    setUnread(store, null);
                }}
            />
        );
    } else if (view.kind === 'message') {
        content = (
            <MessageView
                key={`${folder}/${view.uid}`}
                folder={folder}
                uid={view.uid}
                folders={folders}
                onBack={backToList}
                onReply={reply}
                onForward={forward}
                onChanged={onChanged}
            />
        );
    } else if (view.kind === 'compose') {
        content = (
            <Compose
                draft={view.draft}
                onSent={() => {
                    setNotice(formatMessage(messages.sent));
                    setView(view.back.kind === 'compose' ? {kind: 'list'} : view.back);
                    onChanged();
                }}
                onClose={() => {
                    // The drafts folder may have changed
                    setView(view.back);
                    setReloadKey((k) => k + 1);
                    loadFolders();
                }}
            />
        );
    } else {
        content = (
            <MessageList
                folder={folder}
                folders={folders}
                reloadKey={reloadKey}
                onFolderChange={(name) => {
                    setNotice('');
                    setFolder(name);
                }}
                onOpen={open}
                onCompose={() => setView({kind: 'compose', draft: emptyDraft, back: {kind: 'list'}})}
                onSettings={() => setView({kind: 'settings'})}
                onRefresh={refresh}
                onChanged={onChanged}
            />
        );
    }

    return (
        <div className={own('root', {fusion: isFusionUI(), classic: !isFusionUI()})}>
            {notice && (
                <div
                    className={own('notice', 'toast')}
                    role='status'
                >
                    {notice}
                </div>
            )}
            {error && loaded && <div className={own('error', 'toast')}>{error}</div>}
            {content}
        </div>
    );
}
