/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import { describe, expect, test } from 'bun:test';
import { parseChannelIdRange } from './channelRange';

describe('channel deletion range', () => {
  test('preserves inclusive, single and sparse ranges without expanding IDs', () => {
    expect(parseChannelIdRange(' 00100 ', '200')).toEqual({
      start_id: 100,
      end_id: 200,
    });
    expect(parseChannelIdRange('5', '5')).toEqual({ start_id: 5, end_id: 5 });
    expect(parseChannelIdRange('1', '9007199254740991')).toEqual({
      start_id: 1,
      end_id: Number.MAX_SAFE_INTEGER,
    });
  });

  test('rejects incomplete, reversed, non-integer and imprecise input', () => {
    for (const [start, end] of [
      ['', '5'],
      ['1', ''],
      [null, '5'],
      [' ', '5'],
      ['0', '5'],
      ['-1', '5'],
      ['2', '1'],
      ['1.5', '5'],
      ['1', '5.1'],
      ['1.0', '5'],
      ['abc', '5'],
      ['1e2', '200'],
      ['0x10', '20'],
      ['NaN', '5'],
      ['1', 'Infinity'],
      ['1', '9007199254740992'],
      ['9007199254740993', '9007199254740994'],
      ['1', '9'.repeat(400)],
    ]) {
      expect(parseChannelIdRange(start, end)).toBeNull();
    }
  });
});
