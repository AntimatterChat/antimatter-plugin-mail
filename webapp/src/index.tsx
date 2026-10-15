// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import React from 'react';
import {FormattedMessage} from 'react-intl';

import {Client4} from 'mattermost-redux/client';
import {getCurrentUserId} from 'mattermost-redux/selectors/entities/users';

import {loadUnread, setUnread} from './actions';
import {mailIconURL} from './app_bar_icon';
import MailPanel from './components/mail_panel';
import manifest from './manifest';
import reducer, {getUnread} from './reducer';
import type {PluginClass, PluginRegistry, PluginStore, RightHandSidebarRegistration, WebSocketMessage} from './types/host';
import {registerTranslations} from './translations';

import './styles.css';

// The unread count is asked for every few minutes while the app is open, which also keeps the
// inbox watched by the server (it stops after 15 minutes without a request).
const UNREAD_HEARTBEAT = 4 * 60 * 1000;

export default class Plugin implements PluginClass {
    private unsubscribers: Array<() => void> = [];
    private appBarId = '';

    public initialize(registry: PluginRegistry, store: PluginStore) {
        registerTranslations(registry);

        if (window.basename) {
            Client4.setUrl(window.basename);
        }

        registry.registerReducer(reducer);

        const rhs = registry.registerRightHandSidebarComponent(
            MailPanel,
            <FormattedMessage
                id='mail.title'
                defaultMessage='Mail'
            />,
        );
        this.registerAppBar(registry, store, rhs);

        registry.registerWebSocketEventHandler(`custom_${manifest.id}_unread`, (msg: WebSocketMessage) => {
            if (typeof msg.data.unread === 'number') {
                setUnread(store, msg.data.unread);
            }
        });
        registry.registerReconnectHandler(() => loadUnread(store));

        this.watchUnread(store);
    }

    public uninitialize() {
        for (const unsubscribe of this.unsubscribers) {
            unsubscribe();
        }
        this.unsubscribers = [];
    }

    // registerAppBar adds Mail to the app bar, with the unread count as a badge. The app bar takes
    // an image URL, so the icon is registered again when the count changes.
    private registerAppBar(registry: PluginRegistry, store: PluginStore, rhs: RightHandSidebarRegistration) {
        let shown: number | null | undefined;
        const update = () => {
            const unread = getUnread(store.getState());
            if (unread === shown) {
                return;
            }
            shown = unread;
            if (this.appBarId) {
                registry.unregisterComponent(this.appBarId);
            }
            this.appBarId = registry.registerAppBarComponent({
                iconUrl: mailIconURL(unread),
                tooltipText: (
                    <FormattedMessage
                        id='mail.title'
                        defaultMessage='Mail'
                    />
                ),
                action: () => store.dispatch(rhs.toggleRHSPlugin),
            });
        };
        update();
        this.unsubscribers.push(store.subscribe(update));
    }

    private watchUnread(store: PluginStore) {
        const load = () => {
            if (getCurrentUserId(store.getState())) {
                loadUnread(store);
            }
        };
        load();

        // The user may not be logged in yet when the plugin starts
        let loggedIn = Boolean(getCurrentUserId(store.getState()));
        this.unsubscribers.push(store.subscribe(() => {
            const now = Boolean(getCurrentUserId(store.getState()));
            if (now && !loggedIn) {
                load();
            }
            loggedIn = now;
        }));

        const interval = window.setInterval(load, UNREAD_HEARTBEAT);
        this.unsubscribers.push(() => window.clearInterval(interval));
    }
}

window.registerPlugin(manifest.id, new Plugin());
