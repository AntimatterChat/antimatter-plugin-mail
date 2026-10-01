// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import React, {useState} from 'react';
import {useIntl} from 'react-intl';

import {am, own} from '../class_names';
import * as client from '../client';
import type {Account, AccountRequest, Security, ServerSettings} from '../client';

import {messages} from './messages';

const defaultPorts: Record<'imap' | 'smtp', Record<Security, number>> = {
    imap: {tls: 993, starttls: 143, none: 143},
    smtp: {tls: 465, starttls: 587, none: 25},
};

type Props = {
    account: Account | null;
    allowInsecure: boolean;
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

// AccountForm connects a mailbox, or changes its settings.
export default function AccountForm({account, allowInsecure, onSaved, onCancel, onDisconnected}: Props) {
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

    let submitLabel = account ? messages.save : messages.connect;
    if (saving) {
        submitLabel = messages.connecting;
    }

    const domain = email.includes('@') ? email.slice(email.indexOf('@') + 1) : '';

    const save = async (e: React.FormEvent) => {
        e.preventDefault();
        setSaving(true);
        setError('');
        const request: AccountRequest = {
            email: email.trim(),
            name: name.trim(),
            imap: {host: imap.host.trim(), port: Number(imap.port), security: imap.security, username: imap.username.trim() || email.trim()},
            smtp: {host: smtp.host.trim(), port: Number(smtp.port), security: smtp.security, username: smtp.username.trim()},
            imap_password: imap.password,
            smtp_password: smtp.password,
        };
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

    return (
        <form
            className={am('app-panel')}
            onSubmit={save}
        >
            <div className={own('form-head')}>
                <h3>{formatMessage(account ? messages.settingsTitle : messages.setupTitle)}</h3>
                {!account && <p>{formatMessage(messages.setupIntro)}</p>}
            </div>
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
            {serverFields('imap', imap, setIMAP)}
            {serverFields('smtp', smtp, setSMTP)}
            {error && <div className={own('error')}>{error}</div>}
            <small className={own('note')}>{formatMessage(messages.passwordsStored)}</small>
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
        </form>
    );
}
