// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import {fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import React from 'react';
import {IntlProvider} from 'react-intl';

import * as client from '../client';
import type {MessageSummary} from '../client';

import {FUSION_LAYER_ID} from './fusion/fusion_ui';
import MessageList from './message_list';

jest.mock('../client', () => ({
    getMessages: jest.fn(),
    setFlag: jest.fn(),
    deleteMessages: jest.fn(),
}));

const mocked = client as jest.Mocked<typeof client>;

function summary(uid: number, subject: string, unread: boolean): MessageSummary {
    return {
        uid,
        from: [{name: 'Marie', email: 'marie@example.com'}],
        to: [],
        subject,
        date: 0,
        size: 10,
        unread,
        flagged: false,
        answered: false,
        draft: false,
        has_attachments: false,
        snippet: '',
    };
}

function renderList(onChanged = jest.fn(), folder: 'INBOX' | 'Trash' = 'INBOX') {
    render(
        <IntlProvider locale='en'>
            <MessageList
                folder={folder}
                folders={[{name: 'INBOX', role: 'inbox', total: 3, unread: 2}, {name: 'Trash', role: 'trash', total: 3, unread: 0}]}
                reloadKey={0}
                onFolderChange={jest.fn()}
                onOpen={jest.fn()}
                onCompose={jest.fn()}
                onSettings={jest.fn()}
                onRefresh={jest.fn()}
                onChanged={onChanged}
            />
        </IntlProvider>,
    );
    return onChanged;
}

describe('MessageList selection', () => {
    beforeEach(() => {
        mocked.getMessages.mockResolvedValue({
            folder: 'INBOX',
            total: 3,
            page: 0,
            per_page: 50,
            messages: [summary(3, 'Beam', true), summary(2, 'Lunch', true), summary(1, 'Notes', false)],
        });
        mocked.setFlag.mockResolvedValue({});
        mocked.deleteMessages.mockResolvedValue({});
    });

    test('marks the selected messages as read', async () => {
        const onChanged = renderList();
        fireEvent.click(await screen.findByLabelText('Select “Beam”'));
        fireEvent.click(screen.getByLabelText('Select “Lunch”'));
        expect(screen.getByText('2 selected')).toBeInTheDocument();

        fireEvent.click(screen.getByText('Mark as read'));
        await waitFor(() => expect(onChanged).toHaveBeenCalled());
        expect(mocked.setFlag).toHaveBeenCalledWith('INBOX', [3, 2], 'seen', true);
        expect(screen.queryByText('2 selected')).not.toBeInTheDocument();
        expect(screen.queryByTitle('Unread')).not.toBeInTheDocument();
    });

    test('selects all and deletes', async () => {
        renderList();
        fireEvent.click(await screen.findByLabelText('Select all'));
        expect(screen.getByText('3 selected')).toBeInTheDocument();

        fireEvent.click(screen.getByLabelText('Delete'));
        await waitFor(() => expect(screen.getByText('No messages here.')).toBeInTheDocument());
        expect(mocked.deleteMessages).toHaveBeenCalledWith('INBOX', [3, 2, 1]);
    });
});

describe('MessageList search', () => {
    test('searches the folder', async () => {
        mocked.getMessages.mockResolvedValue({folder: 'INBOX', total: 0, page: 0, per_page: 50, messages: []});
        renderList();
        await screen.findByText('No messages here.');

        fireEvent.click(screen.getByLabelText('Search'));
        fireEvent.change(screen.getByLabelText('Search this folder'), {target: {value: ' pizza '}});
        mocked.getMessages.mockResolvedValue({folder: 'INBOX', query: 'pizza', total: 1, page: 0, per_page: 50, messages: [summary(2, 'Lunch', false)]});
        fireEvent.submit(screen.getByRole('search'));

        expect(await screen.findByText('1 message contains “pizza”')).toBeInTheDocument();
        expect(mocked.getMessages).toHaveBeenLastCalledWith('INBOX', 0, 50, 'pizza');

        fireEvent.click(screen.getByLabelText('Clear the search'));
        await waitFor(() => expect(mocked.getMessages).toHaveBeenLastCalledWith('INBOX', 0, 50, ''));
    });
});

const setWebUI = (ui?: 'classic' | 'fusion') => {
    window.antimatterWebUI = ui;
    if (ui) {
        document.documentElement.dataset.amWebUi = ui;
    } else {
        delete document.documentElement.dataset.amWebUi;
    }
};

// The list's toolbar buttons that show only an icon, and their sprite symbols under Fusion (null: the app's own).
const ICON_BUTTONS: Array<[string, string | null]> = [['Search', 'search'], ['New message', 'pen'], ['Refresh', null], ['Account settings', 'cog']];

describe('MessageList under Fusion', () => {
    let native: jest.SpyInstance;
    let layer: HTMLElement;

    beforeEach(() => {
        setWebUI('fusion');
        layer = document.createElement('div');
        layer.id = FUSION_LAYER_ID;
        document.body.appendChild(layer);
        native = jest.spyOn(window, 'confirm');
        mocked.getMessages.mockResolvedValue({
            folder: 'Trash',
            total: 2,
            page: 0,
            per_page: 50,
            messages: [{...summary(3, 'Beam', true), has_attachments: true}, summary(2, 'Lunch', false)],
        });
        mocked.deleteMessages.mockReset();
        mocked.deleteMessages.mockResolvedValue({});
    });

    afterEach(() => {
        native.mockRestore();
        layer.remove();
        setWebUI(undefined);
    });

    test('asks with Fusion\'s dialog before deleting messages of the trash for good, and deletes only on yes', async () => {
        renderList(jest.fn(), 'Trash');
        fireEvent.click(await screen.findByLabelText('Select all'));
        fireEvent.click(screen.getByRole('button', {name: 'Delete'}));

        let dialog = await screen.findByRole('alertdialog');
        expect(layer).toContainElement(dialog);
        expect(within(dialog).getByRole('heading')).toHaveTextContent('Delete these 2 messages for good?');
        expect(dialog).toHaveTextContent('They’re already in the trash. This can’t be undone.');
        fireEvent.click(within(dialog).getByRole('button', {name: 'Cancel'}));
        await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
        expect(mocked.deleteMessages).not.toHaveBeenCalled();
        expect(screen.getByText('2 selected')).toBeInTheDocument();

        fireEvent.click(screen.getByRole('button', {name: 'Delete'}));
        dialog = await screen.findByRole('alertdialog');
        const yes = within(dialog).getByRole('button', {name: 'Delete'});
        expect(yes).toHaveClass('am-btn', 'am-danger');
        fireEvent.click(yes);
        await waitFor(() => expect(mocked.deleteMessages).toHaveBeenCalledWith('Trash', [3, 2]));
        expect(native).not.toHaveBeenCalled();
    });

    test('deleting from another folder (to the trash) asks nothing', async () => {
        renderList();
        fireEvent.click(await screen.findByLabelText('Select all'));
        fireEvent.click(screen.getByRole('button', {name: 'Delete'}));
        await waitFor(() => expect(mocked.deleteMessages).toHaveBeenCalledWith('INBOX', [3, 2]));
        expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    });

    test.each(ICON_BUTTONS)('the %s button has Fusion\'s tooltip and icon', async (label, symbol) => {
        renderList();
        await screen.findByText(/Beam/);
        const button = screen.getByRole('button', {name: label});
        expect(button).toHaveAttribute('data-am-tip', label);
        expect(button).not.toHaveAttribute('title');
        if (symbol) {
            expect(button.querySelector('use')).toHaveAttribute('href', `#am-i-${symbol}`);
        } else {
            expect(button.querySelector('use')).toBeNull();
        }
    });

    test('the attachment and unread marks have Fusion\'s tooltips and keep their names; nothing has a title', async () => {
        renderList();
        await screen.findByText(/Beam/);
        expect(screen.getByRole('img', {name: 'Has attachments'})).toHaveAttribute('data-am-tip', 'Has attachments');
        expect(screen.getByRole('img', {name: 'Unread'})).toHaveAttribute('data-am-tip', 'Unread');
        expect(screen.getByLabelText('Select all')).toHaveAttribute('data-am-tip', 'Select all');
        expect(document.querySelector('[title]')).toBeNull();
    });

    test('loads with a quiet busy line, and shows an empty folder as Fusion\'s empty state', async () => {
        let resolve: (value: Awaited<ReturnType<typeof client.getMessages>>) => void = () => {};
        mocked.getMessages.mockReturnValue(new Promise((r) => {
            resolve = r;
        }));
        renderList();
        const loading = screen.getByRole('status');
        expect(loading).toHaveClass('am-empty', 'amfx-empty', 'amfx-busy');

        resolve({folder: 'INBOX', total: 0, page: 0, per_page: 50, messages: []});
        const empty = await screen.findByTestId('mail-folder-empty');
        expect(empty).toHaveClass('am-empty', 'amfx-empty');
        expect(empty).toHaveTextContent('No messages here.');
        expect(empty.querySelector('.amfx-empty-ic use')).toHaveAttribute('href', '#am-i-inbox');
    });
});
