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
import { parseStringMap } from './stringMap';

describe('keyword replacement string mappings', () => {
  test('preserves deletion, numeric strings, whitespace, and special keys', () => {
    const text =
      '{"苹果":"橘子","delete":"","0":"false"," line\\n":"  ","__proto__":"constructor"}';
    const rules = parseStringMap(text);
    expect(rules['0']).toBe('false');
    expect(rules.delete).toBe('');
    expect(rules[' line\n']).toBe('  ');
    expect(rules.__proto__).toBe('constructor');
    expect(parseStringMap(JSON.stringify(rules))).toEqual(rules);
  });

  test('allows clearing all rules', () => {
    expect(parseStringMap('')).toEqual({});
    expect(parseStringMap('  ')).toEqual({});
    expect(parseStringMap('{}')).toEqual({});
  });

  test('rejects invalid drafts instead of reusing previously valid rules', () => {
    for (const input of [
      '{',
      'null',
      '[]',
      'false',
      '"word"',
      '{"":"x"}',
      '{"word":false}',
      '{"word":0}',
      '{"word":null}',
      '{"word":{}}',
      '{"word":[]}',
    ]) {
      expect(() => parseStringMap(input)).toThrow();
    }
  });
});
