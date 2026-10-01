// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import * as client from './client';
import {RECEIVED_UNREAD} from './reducer';
import type {PluginStore} from './types/host';

// logError reports an error of a background request. Errors of user actions are shown instead.
export function logError(message: string, err: unknown) {
    // eslint-disable-next-line no-console
    console.error(`[mail] ${message}`, err);
}

export function setUnread(store: PluginStore, unread: number | null) {
    store.dispatch({type: RECEIVED_UNREAD, data: unread});
}

// loadUnread loads the unread count of the inbox. Asking for it also keeps the inbox watched
// by the server, which then sends the count when it changes.
export async function loadUnread(store: PluginStore, refresh = false) {
    try {
        setUnread(store, await client.getUnread(refresh));
    } catch (err) {
        if (err instanceof client.ClientError && err.status === 409) {
            // No account
            setUnread(store, null);
            return;
        }
        logError('failed to load the unread count', err);
    }
}
