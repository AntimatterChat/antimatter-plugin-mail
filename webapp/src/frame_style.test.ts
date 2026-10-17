// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import {CLASSIC_LINK_COLOR, contrastOnWhite, frameLinkColor, parseColor} from './frame_style';

const setWebUI = (ui?: 'classic' | 'fusion') => {
    window.antimatterWebUI = ui;
    if (ui) {
        document.documentElement.dataset.amWebUi = ui;
    } else {
        delete document.documentElement.dataset.amWebUi;
    }
};

// The page's --am-anti, as Fusion's theme sets it.
const withLinkToken = (value: string) => jest.spyOn(window, 'getComputedStyle').mockReturnValue({
    getPropertyValue: (name: string) => (name === '--am-anti' ? value : ''),
} as unknown as CSSStyleDeclaration);

afterEach(() => {
    jest.restoreAllMocks();
    setWebUI(undefined);
});

describe('parseColor', () => {
    test('reads the colours a computed custom property holds', () => {
        expect(parseColor(' #0E7490')).toEqual([14, 116, 144]);
        expect(parseColor('#fff')).toEqual([255, 255, 255]);
        expect(parseColor('rgb(109, 40, 217)')).toEqual([109, 40, 217]);
        expect(parseColor('rgba(109 40 217 / 0.5)')).toEqual([109, 40, 217]);
    });

    test('refuses anything else', () => {
        expect(parseColor('')).toBeNull();
        expect(parseColor('red')).toBeNull();
        expect(parseColor('#12345')).toBeNull();
        expect(parseColor('rgb(300, 0, 0)')).toBeNull();
        expect(parseColor('#0E7490;}body{display:none')).toBeNull();
    });
});

describe('contrastOnWhite', () => {
    test('is the WCAG contrast ratio against white', () => {
        expect(contrastOnWhite([0, 0, 0])).toBeCloseTo(21, 0);
        expect(contrastOnWhite([255, 255, 255])).toBeCloseTo(1, 5);
        expect(contrastOnWhite([14, 116, 144])).toBeGreaterThan(4.5);
        expect(contrastOnWhite([34, 211, 238])).toBeLessThan(4.5);
    });
});

describe('frameLinkColor', () => {
    test('under the classic UI, is the colour it always had', () => {
        setWebUI('classic');
        withLinkToken('#6D28D9');
        expect(frameLinkColor()).toBe(CLASSIC_LINK_COLOR);
    });

    test('under Fusion, is Fusion\'s link colour (--am-anti) when it reads on the frame\'s white', () => {
        setWebUI('fusion');
        withLinkToken(' #0E7490');
        expect(frameLinkColor()).toBe('#0e7490');

        jest.restoreAllMocks();
        withLinkToken('rgb(109, 40, 217)');
        expect(frameLinkColor()).toBe('#6d28d9');
    });

    test('under Fusion\'s dark themes, whose link colour is for dark surfaces, is Fusion\'s light link colour', () => {
        setWebUI('fusion');
        withLinkToken('#22D3EE');
        expect(frameLinkColor()).toBe('#0e7490');
    });

    test('never passes on anything but a colour, nor Mattermost\'s blue', () => {
        setWebUI('fusion');
        withLinkToken('red;}a{background:url(https://tracker.example/x)');
        expect(frameLinkColor()).toBe('#0e7490');

        jest.restoreAllMocks();
        withLinkToken('');
        expect(frameLinkColor()).toBe('#0e7490');
        expect(frameLinkColor()).not.toBe(CLASSIC_LINK_COLOR);
    });
});
