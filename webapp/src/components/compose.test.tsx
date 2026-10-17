// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import {fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import React from 'react';
import {IntlProvider} from 'react-intl';

import * as client from '../client';
import {emptyDraft} from '../utils';

import Compose from './compose';
import {FUSION_LAYER_ID} from './fusion/fusion_ui';

jest.mock('../client', () => ({
    saveDraft: jest.fn(),
    deleteDraft: jest.fn(),
    sendMessage: jest.fn(),
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

function renderCompose() {
    const onClose = jest.fn();
    render(
        <IntlProvider locale='en'>
            <Compose
                draft={emptyDraft}
                onSent={jest.fn()}
                onClose={onClose}
            />
        </IntlProvider>,
    );
    fireEvent.change(screen.getByLabelText('Subject'), {target: {value: 'Beam schedule'}});
    return onClose;
}

describe('Compose: discarding', () => {
    let native: jest.SpyInstance;

    beforeEach(() => {
        mocked.deleteDraft.mockReset();
        native = jest.spyOn(window, 'confirm');
    });

    afterEach(() => {
        native.mockRestore();
        setWebUI(undefined);
        document.getElementById(FUSION_LAYER_ID)?.remove();
    });

    test('under Fusion, asks with Fusion\'s dialog, and discards only on yes', async () => {
        setWebUI('fusion');
        const layer = document.createElement('div');
        layer.id = FUSION_LAYER_ID;
        document.body.appendChild(layer);
        const onClose = renderCompose();

        fireEvent.click(screen.getByRole('button', {name: 'Discard'}));
        let dialog = await screen.findByRole('alertdialog');
        expect(layer).toContainElement(dialog);
        expect(within(dialog).getByRole('heading')).toHaveTextContent('Discard this message?');
        expect(dialog).toHaveTextContent('What you wrote is lost, and its saved draft is deleted too.');
        fireEvent.click(within(dialog).getByRole('button', {name: 'Cancel'}));
        await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
        expect(onClose).not.toHaveBeenCalled();
        expect(screen.getByLabelText('Subject')).toHaveValue('Beam schedule');

        fireEvent.click(screen.getByRole('button', {name: 'Discard'}));
        dialog = await screen.findByRole('alertdialog');
        fireEvent.click(within(dialog).getByRole('button', {name: 'Discard'}));
        await waitFor(() => expect(onClose).toHaveBeenCalled());
        expect(native).not.toHaveBeenCalled();
    });

    test('under Fusion, the toolbar\'s back button has Fusion\'s tooltip', () => {
        setWebUI('fusion');
        renderCompose();
        const back = screen.getByRole('button', {name: 'Save the draft and close'});
        expect(back).toHaveAttribute('data-am-tip', 'Save the draft and close');
        expect(back).not.toHaveAttribute('title');
    });

    test('under the classic UI, keeps the browser\'s confirmation', async () => {
        setWebUI('classic');
        native.mockReturnValue(false);
        const onClose = renderCompose();

        fireEvent.click(screen.getByRole('button', {name: 'Discard'}));
        await waitFor(() => expect(native).toHaveBeenCalledWith('Discard this message?\n\nWhat you wrote is lost, and its saved draft is deleted too.'));
        expect(onClose).not.toHaveBeenCalled();
        expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    });
});
