// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import {fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import React from 'react';
import {IntlProvider} from 'react-intl';

import * as client from '../client';

import AccountForm from './account_form';
import {FUSION_LAYER_ID} from './fusion/fusion_ui';

jest.mock('../client', () => ({
    getAccount: jest.fn(),
    saveAccount: jest.fn(),
    deleteAccount: jest.fn(),
}));

const mocked = client as jest.Mocked<typeof client>;

const setWebUI = (ui?: 'classic' | 'fusion') => {
    window.antimatterWebUI = ui;
    if (ui) {
        document.documentElement.dataset.amWebUi = ui;
    } else {
        delete document.documentElement.dataset.amWebUi;
    }
};

function renderForm() {
    const onDisconnected = jest.fn();
    render(
        <IntlProvider locale='en'>
            <AccountForm
                account={{
                    email: 'maya@example.com',
                    name: 'Maya',
                    imap: {host: 'imap.example.com', port: 993, security: 'tls', username: 'maya'},
                    smtp: {host: 'smtp.example.com', port: 465, security: 'tls', username: 'maya'},
                }}
                allowInsecure={false}
                providers={[]}
                onSaved={jest.fn()}
                onCancel={null}
                onDisconnected={onDisconnected}
            />
        </IntlProvider>,
    );
    return onDisconnected;
}

describe('AccountForm: disconnecting', () => {
    let native: jest.SpyInstance;

    beforeEach(() => {
        mocked.deleteAccount.mockReset();
        mocked.deleteAccount.mockResolvedValue({});
        native = jest.spyOn(window, 'confirm');
    });

    afterEach(() => {
        native.mockRestore();
        setWebUI(undefined);
        document.getElementById(FUSION_LAYER_ID)?.remove();
    });

    test('under Fusion, asks with Fusion\'s dialog, and disconnects only on yes', async () => {
        setWebUI('fusion');
        const layer = document.createElement('div');
        layer.id = FUSION_LAYER_ID;
        document.body.appendChild(layer);
        const onDisconnected = renderForm();

        fireEvent.click(screen.getByRole('button', {name: 'Disconnect'}));
        let dialog = await screen.findByRole('alertdialog');
        expect(layer).toContainElement(dialog);
        expect(within(dialog).getByRole('heading')).toHaveTextContent('Disconnect your mailbox?');
        expect(dialog).toHaveTextContent('Your mail stays on your mail server.');
        fireEvent.click(within(dialog).getByRole('button', {name: 'Cancel'}));
        await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
        expect(mocked.deleteAccount).not.toHaveBeenCalled();

        fireEvent.click(screen.getByRole('button', {name: 'Disconnect'}));
        dialog = await screen.findByRole('alertdialog');
        fireEvent.click(within(dialog).getByRole('button', {name: 'Disconnect'}));
        await waitFor(() => expect(onDisconnected).toHaveBeenCalled());
        expect(mocked.deleteAccount).toHaveBeenCalledTimes(1);
        expect(native).not.toHaveBeenCalled();
    });

    test('under the classic UI, keeps the browser\'s confirmation', async () => {
        setWebUI('classic');
        native.mockReturnValue(false);
        renderForm();

        fireEvent.click(screen.getByRole('button', {name: 'Disconnect'}));
        await waitFor(() => expect(native).toHaveBeenCalledWith('Disconnect your mailbox?\n\nAntimatter stops showing your mail. Your mail stays on your mail server.'));
        expect(mocked.deleteAccount).not.toHaveBeenCalled();
    });
});
