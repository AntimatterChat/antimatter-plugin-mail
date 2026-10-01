// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import {initials, listDate, parseAddresses} from './utils';

describe('parseAddresses', () => {
    test('splits on commas and semicolons, outside quotes and angle brackets', () => {
        expect(parseAddresses('a@example.com, "Laurent, Marie" <marie@example.com>; b@example.com,')).toEqual([
            {email: 'a@example.com'},
            {email: '"Laurent, Marie" <marie@example.com>'},
            {email: 'b@example.com'},
        ]);
        expect(parseAddresses('  ')).toEqual([]);
    });
});

describe('initials', () => {
    test('uses the first and last words', () => {
        expect(initials('Marie Laurent')).toBe('ML');
        expect(initials('Journal of Applied Physics')).toBe('JP');
        expect(initials('supplier@example.com')).toBe('SU');
        expect(initials('')).toBe('?');
    });
});

describe('listDate', () => {
    const now = new Date(2026, 9, 1, 15, 0);
    test('shows the time today, yesterday, the weekday this week, the date before', () => {
        expect(listDate(new Date(2026, 9, 1, 11, 52).getTime(), now).kind).toBe('time');
        expect(listDate(new Date(2026, 8, 30, 23, 0).getTime(), now).kind).toBe('yesterday');
        expect(listDate(new Date(2026, 8, 26, 9, 0).getTime(), now).kind).toBe('weekday');
        expect(listDate(new Date(2026, 8, 20, 9, 0).getTime(), now).kind).toBe('date');
    });
});
