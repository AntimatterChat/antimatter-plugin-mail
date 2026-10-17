// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import {fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import React from 'react';
import {IntlProvider} from 'react-intl';

import * as client from '../client';
import type {Folder, FullMessage} from '../client';

import {FUSION_LAYER_ID} from './fusion/fusion_ui';
import MessageView, {frameDocument} from './message_view';

jest.mock('react-redux', () => ({useSelector: (selector: (state: object) => unknown) => selector({})}));
jest.mock('mattermost-redux/selectors/entities/channels', () => ({
    getMyChannels: () => [],
    getCurrentChannelId: () => '',
}));
jest.mock('../client', () => ({
    getMessage: jest.fn(),
    deleteMessages: jest.fn(),
    attachmentURL: jest.fn(() => '/attachment'),
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

const message: FullMessage = {
    folder: 'Trash',
    uid: 7,
    message_id: 'beam@example.com',
    in_reply_to: [],
    references: [],
    from: [{name: 'Marie Laurent', email: 'marie@example.com'}],
    to: [{name: 'Ada', email: 'ada@example.com'}],
    cc: [],
    bcc: [],
    reply_to: [],
    subject: 'Beam schedule',
    date: 0,
    unread: false,
    flagged: false,
    draft: false,
    text: 'See the schedule.',
    html: '<p>See <a href="https://example.com/schedule">the schedule</a>.</p>',
    remote_images: 0,
    attachments: [],
};

const folders: Folder[] = [{name: 'INBOX', role: 'inbox', total: 1, unread: 0}, {name: 'Trash', role: 'trash', total: 1, unread: 0}];

// The toolbar's icon buttons, once the message is shown.
const ICON_BUTTONS = ['Back to the list', 'Reply', 'Reply all', 'Forward', 'Share to a channel', 'Mark as unread', 'Delete'];

function renderView(onBack = jest.fn()) {
    render(
        <IntlProvider locale='en'>
            <MessageView
                folder='Trash'
                uid={7}
                folders={folders}
                onBack={onBack}
                onReply={jest.fn()}
                onForward={jest.fn()}
                onChanged={jest.fn()}
            />
        </IntlProvider>,
    );
    return onBack;
}

// jsdom has no ResizeObserver, which the email's frame uses to grow with its content.
if (!('ResizeObserver' in window)) {
    Object.assign(window, {ResizeObserver: class {
        observe() {}
        disconnect() {}
    }});
}

describe('MessageView', () => {
    let native: jest.SpyInstance;

    beforeEach(() => {
        mocked.getMessage.mockResolvedValue(message);
        mocked.deleteMessages.mockReset();
        mocked.deleteMessages.mockResolvedValue({});
        native = jest.spyOn(window, 'confirm');
    });

    afterEach(() => {
        native.mockRestore();
        setWebUI(undefined);
        document.getElementById(FUSION_LAYER_ID)?.remove();
    });

    describe('under Fusion', () => {
        let layer: HTMLElement;

        beforeEach(() => {
            setWebUI('fusion');
            layer = document.createElement('div');
            layer.id = FUSION_LAYER_ID;
            document.body.appendChild(layer);
        });

        test('shows a quiet busy line while the message loads', () => {
            mocked.getMessage.mockReturnValue(new Promise(() => {}));
            renderView();
            const loading = screen.getByRole('status');
            expect(loading).toHaveClass('am-empty', 'amfx-empty', 'amfx-busy');
            expect(loading).toHaveTextContent('Loading…');
        });

        test('asks with Fusion\'s dialog before deleting a message of the trash for good, and deletes only on yes', async () => {
            const onBack = renderView();
            await screen.findByText('Beam schedule', {selector: 'h3'});

            fireEvent.click(screen.getByRole('button', {name: 'Delete'}));
            let dialog = await screen.findByRole('alertdialog');
            expect(layer).toContainElement(dialog);
            expect(within(dialog).getByRole('heading')).toHaveTextContent('Delete this message for good?');
            expect(dialog).toHaveTextContent('It’s already in the trash. This can’t be undone.');
            fireEvent.click(within(dialog).getByRole('button', {name: 'Cancel'}));
            await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
            expect(mocked.deleteMessages).not.toHaveBeenCalled();

            fireEvent.click(screen.getByRole('button', {name: 'Delete'}));
            dialog = await screen.findByRole('alertdialog');
            fireEvent.click(within(dialog).getByRole('button', {name: 'Delete'}));
            await waitFor(() => expect(mocked.deleteMessages).toHaveBeenCalledWith('Trash', [7]));
            await waitFor(() => expect(onBack).toHaveBeenCalled());
            expect(native).not.toHaveBeenCalled();
        });

        test.each(ICON_BUTTONS)('the %s button has Fusion\'s tooltip and keeps its name', async (label) => {
            renderView();
            await screen.findByText('Beam schedule', {selector: 'h3'});
            const button = screen.getByRole('button', {name: label});
            expect(button).toHaveAttribute('data-am-tip', label);
            expect(button).not.toHaveAttribute('title');
        });

        test('draws Fusion\'s sprite icons, and gives the sender\'s address as Fusion\'s tooltip', async () => {
            renderView();
            await screen.findByText('Beam schedule', {selector: 'h3'});
            expect(screen.getByRole('button', {name: 'Reply'}).querySelector('use')).toHaveAttribute('href', '#am-i-reply');
            expect(screen.getByRole('button', {name: 'Delete'}).querySelector('use')).toHaveAttribute('href', '#am-i-trash');
            const back = screen.getByRole('button', {name: 'Back to the list'}).querySelector('svg')!;
            expect(back.querySelector('use')).toHaveAttribute('href', '#am-i-chev');
            expect(back.style.transform).toBe('rotate(90deg)');
            expect(screen.getByText('Marie Laurent', {selector: 'b'})).toHaveAttribute('data-am-tip', 'marie@example.com');

            // The frame's title names it for screen readers; it's not a tooltip.
            expect(Array.from(document.querySelectorAll('[title]')).map((el) => el.tagName)).toEqual(['IFRAME']);
        });

        test('the email\'s links take Fusion\'s link colour, not Mattermost\'s blue', async () => {
            jest.spyOn(window, 'getComputedStyle').mockReturnValue({
                getPropertyValue: (name: string) => (name === '--am-anti' ? '#0E7490' : ''),
            } as unknown as CSSStyleDeclaration);
            renderView();
            const frame = await screen.findByTitle('Beam schedule');
            const doc = frame.getAttribute('srcdoc') || '';
            expect(doc).toContain('a{color:#0e7490;}');
            expect(doc).not.toContain('#1c58d9');
            (window.getComputedStyle as jest.Mock).mockRestore();
        });
    });

    describe('under the classic UI', () => {
        beforeEach(() => setWebUI('classic'));

        test('keeps the browser\'s confirmation and tooltips', async () => {
            native.mockReturnValue(false);
            renderView();
            await screen.findByText('Beam schedule', {selector: 'h3'});
            expect(screen.getByRole('button', {name: 'Delete'})).toHaveAttribute('title', 'Delete');

            fireEvent.click(screen.getByRole('button', {name: 'Delete'}));
            await waitFor(() => expect(native).toHaveBeenCalledWith('Delete this message for good?\n\nIt’s already in the trash. This can’t be undone.'));
            expect(mocked.deleteMessages).not.toHaveBeenCalled();
            expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
        });
    });
});

describe('frameDocument', () => {
    test('writes the link colour it\'s given into the frame\'s styles', () => {
        expect(frameDocument('<a href="#">x</a>', false, '#123456')).toContain('a{color:#123456;}');
    });
});
