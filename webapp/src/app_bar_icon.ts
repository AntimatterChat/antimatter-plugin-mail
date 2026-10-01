// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

const ICON_COLOR = '#2DD4BF';

// mailIconURL returns the app bar icon. The app bar only takes image URLs.
export function mailIconURL(): string {
    const svg = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 28 28">' +
        `<g transform="translate(1 3)" fill="none" stroke="${ICON_COLOR}" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">` +
        '<rect x="3" y="5" width="18" height="14" rx="2"/><path d="m4 7 8 6 8-6"/></g>' +
        '</svg>';
    return 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg);
}
