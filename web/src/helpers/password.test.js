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
import { getPasswordChecks, isValidLoginPassword } from './password';

describe('new login password policy', () => {
  test('accepts the minimum, 72-byte boundary and multibyte passwords', () => {
    for (const password of [
      'Ab1!cdef',
      'Ab1!' + 'x'.repeat(68),
      'Ab1!' + '中'.repeat(22) + 'xy',
      ' Ab1!xyz ',
    ]) {
      expect(isValidLoginPassword(password)).toBe(true);
    }
  });
  test('rejects short, overlong and incomplete passwords', () => {
    for (const password of [
      '',
      'Ab1!xyz',
      'abcdefgh1!',
      'ABCDEFGH1!',
      'Abcdefgh!',
      'Abcdefgh1',
      'Abcdefg1中',
      'Abcdefg1 ',
      'Ab1!' + 'x'.repeat(69),
      'Ab1!' + '中'.repeat(23),
    ]) {
      expect(isValidLoginPassword(password)).toBe(false);
    }
  });
  test('counts Unicode code points, not UTF-16 code units', () => {
    expect(getPasswordChecks('Ab1!😀😀😀').length).toBe(false);
    expect(getPasswordChecks('Ab1!😀😀😀😀').length).toBe(true);
  });
});
