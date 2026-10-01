// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import React from 'react';
import {FormattedMessage} from 'react-intl';

import {Client4} from 'mattermost-redux/client';

import {mailIconURL} from './app_bar_icon';
import MailPanel from './components/mail_panel';
import manifest from './manifest';
import type {PluginClass, PluginRegistry, PluginStore} from './types/host';

import './styles.css';

export default class Plugin implements PluginClass {
    public initialize(registry: PluginRegistry, store: PluginStore) {
        if (window.basename) {
            Client4.setUrl(window.basename);
        }

        const rhs = registry.registerRightHandSidebarComponent(
            MailPanel,
            <FormattedMessage
                id='mail.title'
                defaultMessage='Mail'
            />,
        );
        registry.registerAppBarComponent({
            iconUrl: mailIconURL(),
            tooltipText: (
                <FormattedMessage
                    id='mail.title'
                    defaultMessage='Mail'
                />
            ),
            action: () => store.dispatch(rhs.toggleRHSPlugin),
        });
    }
}

window.registerPlugin(manifest.id, new Plugin());
