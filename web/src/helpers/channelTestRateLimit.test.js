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

import { beforeAll, describe, expect, test } from 'bun:test';
import i18next from 'i18next';
import { formatChannelTestRateLimit } from './channelTestRateLimit';

const i18n = i18next.createInstance();
beforeAll(async () => {
  await i18n.init({ lng: 'zh', resources: {}, initImmediate: false });
});
const t = i18n.t.bind(i18n);

describe('channel test rate limits', () => {
  test('shows deployment limits and remaining budget', () => {
    expect(
      formatChannelTestRateLimit(
        {
          provider: 'azure',
          limit_requests: '60',
          limit_tokens: '150000',
          remaining_requests: '59',
          remaining_tokens: '149984',
        },
        t,
      ),
    ).toEqual({
      limits: '限速: RPM: 60 / TPM: 150000',
      remaining: '剩余：59 次请求 / 149984 token',
    });
  });

  test('missing headers are unknown, not zero or unlimited', () => {
    for (const value of [null, undefined, {}, { limit_requests: ' ' }]) {
      expect(formatChannelTestRateLimit(value, t)).toEqual({
        limits: '限速: RPM: 上游未返回 / TPM: 上游未返回',
        remaining: '',
      });
    }
  });

  test('remaining values are never used as the limit', () => {
    expect(
      formatChannelTestRateLimit(
        {
          remaining_requests: '0',
          remaining_tokens: 0,
        },
        t,
      ),
    ).toEqual({
      limits: '限速: RPM: 上游未返回 / TPM: 上游未返回',
      remaining: '剩余：0 次请求 / 0 token',
    });
  });

  test('preserves zero limits and missing dimensions independently', () => {
    expect(formatChannelTestRateLimit({ limit_requests: 0 }, t).limits).toBe(
      '限速: RPM: 0 / TPM: 上游未返回',
    );
  });

  test('project token limits use the matching remaining budget', () => {
    expect(
      formatChannelTestRateLimit(
        {
          limit_project_tokens: '10000',
          remaining_project_tokens: '9000',
          remaining_tokens: '123',
        },
        t,
      ).remaining,
    ).toBe('剩余：上游未返回 次请求 / 9000 token');
  });

  test('retains Anthropic input, output and priority limits', () => {
    const result = formatChannelTestRateLimit(
      {
        limit_requests: '100',
        limit_input_tokens: '20000',
        limit_output_tokens: '4000',
        limit_priority_input_tokens: '50000',
        limit_priority_output_tokens: '10000',
      },
      t,
    );
    expect(result.limits).toContain('RPM: 100');
    expect(result.limits).toContain('ITPM: 20000 / OTPM: 4000');
    expect(result.limits).toContain('优先级 ITPM: 50000 / 优先级 OTPM: 10000');
  });
});
