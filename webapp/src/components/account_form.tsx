// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import React, {useCallback, useEffect, useRef, useState} from 'react';
import {useIntl} from 'react-intl';

import {am, own} from '../class_names';
import * as client from '../client';
import type {Account, AccountRequest, OAuthProvider, Security, ServerSettings} from '../client';
import manifest from '../manifest';

import {messages} from './messages';

const defaultPorts: Record<'imap' | 'smtp', Record<Security, number>> = {
    imap: {tls: 993, starttls: 143, none: 143},
    smtp: {tls: 465, starttls: 587, none: 25},
};

type Props = {
    account: Account | null;
    allowInsecure: boolean;
    providers: OAuthProvider[];
    onSaved: (account: Account) => void;
    onCancel: (() => void) | null;
    onDisconnected: () => void;
};

type ServerForm = {
    host: string;
    port: string;
    security: Security;
    username: string;
    password: string;
};

function serverForm(settings: ServerSettings | undefined, kind: 'imap' | 'smtp'): ServerForm {
    const security = settings?.security || (kind === 'imap' ? 'tls' : 'starttls');
    return {
        host: settings?.host || '',
        port: String(settings?.port || defaultPorts[kind][security]),
        security,
        username: settings?.username || '',
        password: '',
    };
}

// oauthMessageType is the type of the message the sign-in window sends when it's done.
const oauthMessageType = `${manifest.id}.oauth`;

// useOAuthSignIn signs in with a provider in a separate window, which the provider sends back to
// the plugin when done. It calls onSignedIn with the connected account.
function useOAuthSignIn(onSignedIn: (account: Account) => void, setError: (error: string) => void) {
    const [waiting, setWaiting] = useState(false);
    const [blockedURL, setBlockedURL] = useState('');
    const popup = useRef<Window | null>(null);

    const finish = useCallback(async () => {
        popup.current = null;
        setWaiting(false);
        try {
            const account = await client.getAccount();
            if (account?.oauth_provider) {
                onSignedIn(account);
            }
        } catch (err) {
            setError((err as Error).message);
        }
    }, [onSignedIn, setError]);

    useEffect(() => {
        const onMessage = (e: MessageEvent) => {
            if (e.origin !== window.location.origin || e.source !== popup.current || e.data?.type !== oauthMessageType) {
                return;
            }
            if (e.data.ok) {
                finish();
            } else {
                setError(String(e.data.error || ''));
            }
        };

        // The window may be closed before it tells how it went
        let timer = 0;
        if (waiting) {
            timer = window.setInterval(() => {
                if (popup.current?.closed) {
                    finish();
                }
            }, 1000);
            window.addEventListener('message', onMessage);
        }
        return () => {
            window.clearInterval(timer);
            window.removeEventListener('message', onMessage);
        };
    }, [waiting, finish, setError]);

    const signIn = async (provider: string) => {
        setError('');
        setBlockedURL('');

        // Browsers only let a click open a window, before the sign-in URL is known
        const opened = window.open('', 'amm-oauth', 'popup,width=520,height=680');
        try {
            const url = await client.startOAuth(provider);
            if (opened) {
                opened.location.href = url;
                popup.current = opened;
                setWaiting(true);
            } else {
                setBlockedURL(url);
            }
        } catch (err) {
            opened?.close();
            setError((err as Error).message);
        }
    };

    return {signIn, waiting, blockedURL};
}

// AccountForm connects a mailbox, or changes its settings.
export default function AccountForm({account, allowInsecure, providers, onSaved, onCancel, onDisconnected}: Props) {
    const {formatMessage} = useIntl();
    const [email, setEmail] = useState(account?.email || '');
    const [name, setName] = useState(account?.name || '');
    const [imap, setIMAP] = useState(() => serverForm(account?.imap, 'imap'));
    const [smtp, setSMTP] = useState(() => {
        const form = serverForm(account?.smtp, 'smtp');

        // The SMTP username is the IMAP one unless it differs
        if (account && form.username === account.imap.username) {
            form.username = '';
        }
        return form;
    });
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState('');
    const oauth = useOAuthSignIn(onSaved, setError);
    const accountProvider = providers.find((p) => p.id === account?.oauth_provider)?.name || account?.oauth_provider || '';

    let submitLabel = account ? messages.save : messages.connect;
    if (saving) {
        submitLabel = messages.connecting;
    }

    const domain = email.includes('@') ? email.slice(email.indexOf('@') + 1) : '';

    const save = async (e: React.FormEvent) => {
        e.preventDefault();
        setSaving(true);
        setError('');
        let request: AccountRequest = {
            email: email.trim(),
            name: name.trim(),
            imap: {host: imap.host.trim(), port: Number(imap.port), security: imap.security, username: imap.username.trim() || email.trim()},
            smtp: {host: smtp.host.trim(), port: Number(smtp.port), security: smtp.security, username: smtp.username.trim()},
            imap_password: imap.password,
            smtp_password: smtp.password,
        };
        if (account?.oauth_provider) {
            // Only the name of an account signed in with a provider changes
            request = {...account, name: name.trim(), imap_password: '', smtp_password: ''};
        }
        try {
            onSaved(await client.saveAccount(request));
        } catch (err) {
            setError((err as Error).message);
        } finally {
            setSaving(false);
        }
    };

    const disconnect = async () => {
        // eslint-disable-next-line no-alert
        if (!window.confirm(formatMessage(messages.disconnectConfirm))) {
            return;
        }
        try {
            await client.deleteAccount();
            onDisconnected();
        } catch (err) {
            setError((err as Error).message);
        }
    };

    const serverFields = (kind: 'imap' | 'smtp', form: ServerForm, setForm: (f: ServerForm) => void) => {
        const id = (field: string) => `amm-${kind}-${field}`;
        return (
            <fieldset className={own('fieldset')}>
                <legend className={am('app-h')}>{formatMessage(kind === 'imap' ? messages.incoming : messages.outgoing)}</legend>
                <label
                    className={am('field')}
                    htmlFor={id('host')}
                >
                    <span>{formatMessage(messages.server)}</span>
                    <input
                        id={id('host')}
                        type='text'
                        required={true}
                        value={form.host}
                        placeholder={domain ? `${kind}.${domain}` : ''}
                        autoComplete='off'
                        spellCheck={false}
                        onChange={(e) => setForm({...form, host: e.target.value})}
                    />
                </label>
                <div className={own('field-row')}>
                    <label
                        className={am('field')}
                        htmlFor={id('security')}
                    >
                        <span>{formatMessage(messages.security)}</span>
                        <select
                            id={id('security')}
                            value={form.security}
                            onChange={(e) => {
                                const security = e.target.value as Security;
                                const wasDefault = Number(form.port) === defaultPorts[kind][form.security];
                                setForm({...form, security, port: wasDefault ? String(defaultPorts[kind][security]) : form.port});
                            }}
                        >
                            <option value='tls'>{formatMessage(messages.securityTLS)}</option>
                            <option value='starttls'>{formatMessage(messages.securityStartTLS)}</option>
                            {(allowInsecure || form.security === 'none') && <option value='none'>{formatMessage(messages.securityNone)}</option>}
                        </select>
                    </label>
                    <label
                        className={am('field')}
                        htmlFor={id('port')}
                    >
                        <span>{formatMessage(messages.port)}</span>
                        <input
                            id={id('port')}
                            type='text'
                            inputMode='numeric'
                            pattern='[0-9]*'
                            required={true}
                            value={form.port}
                            onChange={(e) => setForm({...form, port: e.target.value})}
                        />
                    </label>
                </div>
                <label
                    className={am('field')}
                    htmlFor={id('username')}
                >
                    <span>{formatMessage(messages.username)}</span>
                    <input
                        id={id('username')}
                        type='text'
                        value={form.username}
                        placeholder={kind === 'imap' ? email : imap.username || email}
                        autoComplete='off'
                        spellCheck={false}
                        onChange={(e) => setForm({...form, username: e.target.value})}
                    />
                </label>
                <label
                    className={am('field')}
                    htmlFor={id('password')}
                >
                    <span>{formatMessage(messages.password)}</span>
                    <input
                        id={id('password')}
                        type='password'
                        className={own('password')}
                        value={form.password}
                        required={kind === 'imap' && !account}
                        autoComplete='new-password'
                        onChange={(e) => setForm({...form, password: e.target.value})}
                    />
                    {account && <small>{formatMessage(messages.passwordKeep)}</small>}
                    {kind === 'smtp' && !account && <small>{formatMessage(messages.smtpSame)}</small>}
                </label>
            </fieldset>
        );
    };

    const providerButtons = providers.map((p) => (
        <button
            key={p.id}
            type='button'
            className={am('btn', {primary: !account})}
            disabled={oauth.waiting}
            onClick={() => oauth.signIn(p.id)}
        >
            {formatMessage(account?.oauth_provider === p.id ? messages.signInAgain : messages.signInWith, {provider: p.name})}
        </button>
    ));
    const signInStatus = (
        <>
            {oauth.waiting && <div className={own('notice')}>{formatMessage(messages.signingIn)}</div>}
            {oauth.blockedURL && (
                <div className={own('notice')}>
                    <a
                        className={own('link')}
                        href={oauth.blockedURL}
                        target='_blank'
                        rel='noopener noreferrer'
                    >
                        {formatMessage(messages.signInLink)}
                    </a>
                </div>
            )}
        </>
    );

    const actions = (
        <div className={own('actions')}>
            <button
                type='submit'
                className={am('btn', 'primary')}
                disabled={saving}
            >
                {formatMessage(submitLabel)}
            </button>
            {onCancel && (
                <button
                    type='button'
                    className={am('btn')}
                    onClick={onCancel}
                >
                    {formatMessage(messages.cancel)}
                </button>
            )}
            <span className={am('grow')}/>
            {account && (
                <button
                    type='button'
                    className={am('btn', 'danger')}
                    onClick={disconnect}
                >
                    {formatMessage(messages.disconnect)}
                </button>
            )}
        </div>
    );

    const nameField = (
        <label
            className={am('field')}
            htmlFor='amm-name'
        >
            <span>{formatMessage(messages.name)}</span>
            <input
                id='amm-name'
                type='text'
                value={name}
                autoComplete='name'
                onChange={(e) => setName(e.target.value)}
            />
        </label>
    );

    // An account signed in with a provider has no servers to set
    if (account?.oauth_provider) {
        return (
            <form
                className={am('app-panel')}
                onSubmit={save}
            >
                <div className={own('form-head')}>
                    <h3>{formatMessage(messages.settingsTitle)}</h3>
                    <p>{formatMessage(messages.signedInAs, {provider: accountProvider, email: account.email})}</p>
                </div>
                {nameField}
                <div className={own('actions')}>
                    {providerButtons.filter((b) => b.key === account.oauth_provider)}
                </div>
                {signInStatus}
                {error && <div className={own('error')}>{error}</div>}
                <small className={own('note')}>{formatMessage(messages.oauthStored, {provider: accountProvider})}</small>
                {actions}
            </form>
        );
    }

    return (
        <form
            className={am('app-panel')}
            onSubmit={save}
        >
            <div className={own('form-head')}>
                <h3>{formatMessage(account ? messages.settingsTitle : messages.setupTitle)}</h3>
                {!account && <p>{formatMessage(messages.setupIntro)}</p>}
            </div>
            {providers.length > 0 && (
                <>
                    <div className={own('actions')}>{providerButtons}</div>
                    {signInStatus}
                    <p className={own('note')}>{formatMessage(messages.signInManual)}</p>
                </>
            )}
            <label
                className={am('field')}
                htmlFor='amm-email'
            >
                <span>{formatMessage(messages.email)}</span>
                <input
                    id='amm-email'
                    type='text'
                    inputMode='email'
                    required={true}
                    value={email}
                    autoComplete='email'
                    spellCheck={false}
                    onChange={(e) => setEmail(e.target.value)}
                />
            </label>
            {nameField}
            {serverFields('imap', imap, setIMAP)}
            {serverFields('smtp', smtp, setSMTP)}
            {error && <div className={own('error')}>{error}</div>}
            <small className={own('note')}>{formatMessage(messages.passwordsStored)}</small>
            {actions}
        </form>
    );
}
