// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import {fireEvent, render, screen, waitFor} from '@testing-library/react';
import React from 'react';
import {IntlProvider} from 'react-intl';

import * as client from '../client';
import type {MessageSummary} from '../client';

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

function renderList(onChanged = jest.fn()) {
    render(
        <IntlProvider locale='en'>
            <MessageList
                folder='INBOX'
                folders={[{name: 'INBOX', role: 'inbox', total: 3, unread: 2}]}
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
