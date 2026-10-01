// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import React from 'react';

import {am} from '../class_names';

// The icons of the Fusion mockup the mail panel uses (shapes written as paths).
const paths: Record<string, React.ReactNode> = {
    mail: (<path d='M5 5h14a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2zm-1 2 8 6 8-6'/>),
    pen: (<><path d='M4 20h4L19 9l-4-4L4 16z'/><path d='m13 7 4 4'/></>),
    cog: (<path d='M15.2 12a3.2 3.2 0 1 1-6.4 0 3.2 3.2 0 0 1 6.4 0zM12 2.5v3M12 18.5v3M4.6 4.6l2.1 2.1M17.3 17.3l2.1 2.1M2.5 12h3M18.5 12h3M4.6 19.4l2.1-2.1M17.3 6.7l2.1-2.1'/>),
    undo: (<><path d='M9 14 4 9l5-5'/><path d='M4 9h11a5 5 0 0 1 0 10h-3'/></>),
    reply: (<><path d='M10 7 4 12l6 5'/><path d='M4 12h10a6 6 0 0 1 6 6v1'/></>),
    forward: (<path d='m14 5 6 6-6 6M20 11H9a5 5 0 0 0-5 5v3'/>),
    trash: (<path d='M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3'/>),
    folder: (<path d='M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z'/>),
    send: (<><path d='M4 12 20 4l-6 16-3-7z'/><path d='m11 13 9-9'/></>),
    export: (<><path d='M14 3h7v7M21 3l-9 9'/><path d='M19 14v5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2h5'/></>),
    attach: (<path d='m20 11-8.5 8.5a5 5 0 0 1-7-7L13 4a3.5 3.5 0 0 1 5 5l-8.5 8.5a2 2 0 0 1-3-3L14 7'/>),
    inbox: (<><path d='m3 13 3-8h12l3 8v6a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1z'/><path d='M3 13h5l1 3h6l1-3h5'/></>),
    flag: (<path d='M5 21V4M5 4h12l-2.5 4.5L17 13H5'/>),
    x: (<path d='M6 6l12 12M18 6 6 18'/>),
    chev: (<path d='m6 9 6 6 6-6'/>),
    back: (<path d='M15 6l-6 6 6 6'/>),
    refresh: (<><path d='M20 11a8 8 0 0 0-14.6-4.5L4 8'/><path d='M4 4v4h4M4 13a8 8 0 0 0 14.6 4.5L20 16'/><path d='M20 20v-4h-4'/></>),
};

export type IconName = keyof typeof paths;

type Props = {
    name: IconName;
    size?: 'sm' | 'xs';
};

export default function Icon({name, size}: Props) {
    return (
        <svg
            className={am('ic', size)}
            viewBox='0 0 24 24'
            aria-hidden='true'
        >
            {paths[name]}
        </svg>
    );
}
