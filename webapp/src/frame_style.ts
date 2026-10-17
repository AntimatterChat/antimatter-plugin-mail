// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import {isFusionUI} from './web_ui';

// The colour of links in an email's frame under the classic UI, as before.
export const CLASSIC_LINK_COLOR = '#1c58d9';

// Fusion's link colour on a light surface (--am-anti of its light themes). An email is always shown on white, like in
// any mail app (its own colours expect it), so a dark theme's link colour, made for dark surfaces, would be unreadable
// there: this one is used instead.
export const FUSION_LIGHT_LINK_COLOR = '#0E7490';

// The contrast a link needs against the frame's white background (WCAG AA for text).
const MIN_CONTRAST = 4.5;

// parseColor reads a #rgb, #rrggbb or rgb()/rgba() colour, as a computed custom property holds it.
export function parseColor(value: string): [number, number, number] | null {
    const text = value.trim();
    const short = (/^#([0-9a-f]{3})$/i).exec(text)?.[1];
    if (short) {
        return [0, 1, 2].map((i) => parseInt(short[i] + short[i], 16)) as [number, number, number];
    }
    const long = (/^#([0-9a-f]{6})$/i).exec(text)?.[1];
    if (long) {
        return [0, 2, 4].map((i) => parseInt(long.slice(i, i + 2), 16)) as [number, number, number];
    }
    const rgb = (/^rgba?\(\s*(\d{1,3})[\s,]+(\d{1,3})[\s,]+(\d{1,3})\s*(?:[,/]\s*[\d.]+%?\s*)?\)$/i).exec(text);
    if (rgb) {
        const channels = [rgb[1], rgb[2], rgb[3]].map(Number);
        return channels.every((c) => c <= 255) ? channels as [number, number, number] : null;
    }
    return null;
}

// contrastOnWhite returns the contrast ratio of a colour against white.
export function contrastOnWhite([r, g, b]: [number, number, number]): number {
    const channel = (c: number) => {
        const s = c / 255;
        return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
    };
    const luminance = (0.2126 * channel(r)) + (0.7152 * channel(g)) + (0.0722 * channel(b));
    return 1.05 / (luminance + 0.05);
}

const hex = (rgb: [number, number, number]) => '#' + rgb.map((c) => c.toString(16).padStart(2, '0')).join('');

// frameLinkColor returns the colour of links in an email's frame: under Fusion, Fusion's link colour (--am-anti, read
// from the page, as the frame can't see its custom properties) when it's readable on the frame's white, and Fusion's
// light link colour otherwise; under the classic UI, the colour it always had. The result is always a #rrggbb colour,
// so a theme's value can't add anything else to the frame's style sheet.
export function frameLinkColor(): string {
    if (!isFusionUI()) {
        return CLASSIC_LINK_COLOR;
    }
    let value = '';
    try {
        value = window.getComputedStyle(document.documentElement).getPropertyValue('--am-anti');
    } catch {
        // No styles to read: Fusion's light colour, then.
    }
    const rgb = parseColor(value);
    return rgb && contrastOnWhite(rgb) >= MIN_CONTRAST ? hex(rgb) : FUSION_LIGHT_LINK_COLOR.toLowerCase();
}
