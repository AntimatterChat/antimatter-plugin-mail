// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import type {GlobalState} from '@mattermost/types/store';

import manifest from './manifest';

export const RECEIVED_UNREAD = `${manifest.id}_received_unread`;

export type MailPluginState = {

    // The number of unread messages in the inbox, null when unknown (no account)
    unread: number | null;
};

type Action = {
    type: string;
    data?: unknown;
};

const initialState: MailPluginState = {unread: null};

export default function reducer(state: MailPluginState = initialState, action: Action): MailPluginState {
    switch (action.type) {
    case RECEIVED_UNREAD:
        return {...state, unread: action.data as number | null};
    default:
        return state;
    }
}

export function getPluginState(state: GlobalState): MailPluginState {
    return (state as unknown as Record<string, MailPluginState>)[`plugins-${manifest.id}`] || initialState;
}

export function getUnread(state: GlobalState): number | null {
    return getPluginState(state).unread;
}
