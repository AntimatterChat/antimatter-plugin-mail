// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

const ICON_COLOR = '#2DD4BF';
const BADGE_COLOR = '#F23F43';

// mailIconURL returns the app bar icon, with the unread count as a badge. The app bar only takes
// image URLs, so the badge is drawn into the image.
export function mailIconURL(unread: number | null): string {
    let badge = '';
    if (unread && unread > 0) {
        const label = unread > 99 ? '99+' : String(unread);
        const width = label.length === 1 ? 14 : (label.length * 5) + 6;
        badge = `<rect x="${28 - width}" y="0" width="${width}" height="14" rx="7" fill="${BADGE_COLOR}"/>` +
            `<text x="${28 - (width / 2)}" y="10.5" font-family="system-ui, sans-serif" font-size="10" font-weight="700" fill="#fff" text-anchor="middle">${label}</text>`;
    }
    const svg = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 28 28">' +
        `<g transform="translate(1 3)" fill="none" stroke="${ICON_COLOR}" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">` +
        '<rect x="3" y="5" width="18" height="14" rx="2"/><path d="m4 7 8 6 8-6"/></g>' +
        badge + '</svg>';
    return 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg);
}
