// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import {render, screen} from '@testing-library/react';
import React from 'react';
import {IntlProvider} from 'react-intl';

import * as client from '../client';

import MailPanel, {APP_COLOR} from './mail_panel';

jest.mock('react-redux', () => ({
    useStore: () => ({getState: () => ({}), dispatch: jest.fn(), subscribe: jest.fn()}),
    useSelector: (selector: (state: object) => unknown) => selector({}),
}));
jest.mock('mattermost-redux/selectors/entities/channels', () => ({
    getMyChannels: () => [],
    getCurrentChannelId: () => '',
}));
jest.mock('../actions', () => ({loadUnread: jest.fn(), setUnread: jest.fn()}));
jest.mock('../client', () => ({
    getAccount: jest.fn(),
    getConfig: jest.fn(),
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

const renderPanel = () => render(
    <IntlProvider locale='en'>
        <MailPanel/>
    </IntlProvider>,
);

describe('MailPanel', () => {
    beforeEach(() => {
        mocked.getAccount.mockReturnValue(new Promise(() => {}));
        mocked.getConfig.mockReturnValue(new Promise(() => {}));
    });

    afterEach(() => setWebUI(undefined));

    test('under Fusion, loads with a quiet busy line, in a panel of Mail\'s colour', () => {
        setWebUI('fusion');
        const {container} = renderPanel();
        const loading = screen.getByRole('status');
        expect(loading).toHaveClass('am-empty', 'amfx-empty', 'amfx-busy');
        expect(loading).toHaveTextContent('Loading…');
        expect((container.firstElementChild as HTMLElement).style.getPropertyValue('--am-cc')).toBe(APP_COLOR);
    });

    test('under the classic UI, keeps its own loading line', () => {
        setWebUI('classic');
        renderPanel();
        const loading = screen.getByRole('status');
        expect(loading.className).toBe('amm-empty');
        expect(loading).toHaveTextContent('Loading…');
    });
});
