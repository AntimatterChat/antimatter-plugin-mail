// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import {mailIconURL} from './app_bar_icon';

function svg(url: string) {
    return decodeURIComponent(url.replace('data:image/svg+xml;charset=utf-8,', ''));
}

describe('mailIconURL', () => {
    test('draws the unread count as a badge', () => {
        expect(svg(mailIconURL(null))).not.toContain('<text');
        expect(svg(mailIconURL(0))).not.toContain('<text');
        expect(svg(mailIconURL(3))).toContain('>3</text>');
        expect(svg(mailIconURL(250))).toContain('>99+</text>');
    });
});
