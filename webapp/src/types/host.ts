// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import type React from 'react';
import type {AnyAction, Reducer, Store} from 'redux';

import type {GlobalState} from '@mattermost/types/store';

import type {WebUI} from '../web_ui';

export type RightHandSidebarRegistration = {
    id: string;
    showRHSPlugin: AnyAction;
    hideRHSPlugin: AnyAction;
    toggleRHSPlugin: AnyAction;
};

export type WebSocketMessage<T = Record<string, unknown>> = {
    event: string;
    data: T;
    broadcast: {channel_id: string; team_id: string; user_id: string};
};

// The subset of the host's plugin registry this plugin uses.
export interface PluginRegistry {
    registerReducer(reducer: Reducer): void;
    registerWebSocketEventHandler(event: string, handler: (msg: WebSocketMessage) => void): void;
    registerReconnectHandler(handler: () => void): void;
    registerRightHandSidebarComponent(component: React.ComponentType, title: React.ReactNode): RightHandSidebarRegistration;
    registerAppBarComponent(options: {iconUrl: string; tooltipText: React.ReactNode; action: () => void}): string;
    unregisterComponent(componentId: string): void;
}

export type PluginStore = Store<GlobalState>;

export interface PluginClass {
    initialize(registry: PluginRegistry, store: PluginStore): void | Promise<void>;
    uninitialize?(): void;
}

declare global {
    interface Window {
        registerPlugin(pluginId: string, plugin: PluginClass): void;
        basename?: string;

        // Set by the Antimatter web UIs before plugins load
        antimatterWebUI?: WebUI;
    }
}
