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
import {
  collapseClineMappedModels,
  parseUpstreamUpdateMeta,
} from './upstreamUpdateUtils';
import { isManualModelFetchSupported } from '../../constants/channel.constants';

describe('Cline model synchronization', () => {
  test('fetching mapped upstream IDs never duplicates existing short names', () => {
    const mapping = JSON.stringify({
      'space-bunny-alpha': 'stealth/space-bunny-alpha',
      'deepseek-v4.1-flash': 'cline-free/deepseek-v4.1-flash',
    });
    const models = [
      'space-bunny-alpha',
      'deepseek-v4.1-flash',
      'stealth/space-bunny-alpha',
      'cline-free/deepseek-v4.1-flash',
    ];
    const expected = ['space-bunny-alpha', 'deepseek-v4.1-flash'];
    expect(collapseClineMappedModels(models, mapping)).toEqual(expected);
    expect(
      collapseClineMappedModels([...expected, ...models], mapping),
    ).toEqual(expected);
  });

  test('preserves unrelated mappings and new IDs until backend normalization', () => {
    const models = ['a', 'one/a', 'two/b', 'new/model'];
    const mapping = { a: 'manual/target', b: 'two/b', 'two/b': 'manual/other' };
    expect(collapseClineMappedModels(models, mapping)).toEqual(models);
    expect(collapseClineMappedModels(models, 'invalid json')).toEqual(models);
    expect(mapping).toEqual({
      a: 'manual/target',
      b: 'two/b',
      'two/b': 'manual/other',
    });
  });

  test('defaults to enabled only for Cline, including missing settings', () => {
    for (const settings of [
      null,
      '',
      '{}',
      {},
      { cline_auto_sync_free_models_enabled: null },
    ]) {
      expect(parseUpstreamUpdateMeta(settings, 73).enabled).toBe(true);
      expect(parseUpstreamUpdateMeta(settings, 1).enabled).toBe(false);
    }
  });

  test('retains explicit false after a settings round trip', () => {
    const settings = JSON.stringify({
      cline_auto_sync_free_models_enabled: false,
    });
    expect(parseUpstreamUpdateMeta(settings, '73').enabled).toBe(false);
    expect(isManualModelFetchSupported(73)).toBe(true);
  });

  test('exposes detected additions and removals for the existing update UI', () => {
    expect(
      parseUpstreamUpdateMeta(
        {
          upstream_model_update_last_detected_models: [
            'cline-free/new',
            'cline-free/new',
          ],
          upstream_model_update_last_removed_models: ['old'],
        },
        73,
      ),
    ).toEqual({
      enabled: true,
      pendingAddModels: ['cline-free/new'],
      pendingRemoveModels: ['old'],
    });
  });
});
