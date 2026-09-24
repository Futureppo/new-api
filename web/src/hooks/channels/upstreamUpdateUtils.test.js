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
import { parseUpstreamUpdateMeta } from './upstreamUpdateUtils';
import { isManualModelFetchSupported } from '../../constants/channel.constants';

describe('Cline model synchronization', () => {
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
