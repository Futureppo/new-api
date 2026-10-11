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
import { readFileSync } from 'node:fs';
import {
  CHANNEL_TEST_ENDPOINTS,
  getChannelTestEndpoints,
  channelTestSupportsStream,
} from './channelTestEndpoints';

const options = (type) =>
  getChannelTestEndpoints(type, (key) => key).map(({ value }) => value);

describe('channel test endpoints', () => {
  test('adds Decisions and existing synchronous tests without changing auto detection', () => {
    expect(options(1)[0]).toBe('');
    for (const value of [
      'openai-decisions',
      'moderations',
      'audio-speech',
      'audio-transcription',
    ]) {
      expect(options(1)).toContain(value);
      expect(channelTestSupportsStream(1, value)).toBe(false);
    }
    expect(channelTestSupportsStream(1, '')).toBe(true);
    expect(channelTestSupportsStream(1, 'openai-response')).toBe(true);
    expect(options(3)).not.toContain('openai-decisions');
  });

  test('preserves provider restrictions and limits batch generation to GMICloud', () => {
    expect(options(71)).toEqual(['', 'typesafe-systemone']);
    expect(channelTestSupportsStream(71, '')).toBe(false);
    expect(options(72)).toEqual(['', 'openai', 'anthropic']);
    expect(options('34')).toContain('cohere-chat');
    expect(options(1)).not.toContain('cohere-chat');
    expect(options(67)).toContain('batch-generation');
    expect(options(1)).not.toContain('batch-generation');
    expect(channelTestSupportsStream(67, 'batch-generation')).toBe(false);
  });

  test('the dialog catalog covers every backend channel test endpoint', () => {
    const source = readFileSync(
      new URL('../../../constant/endpoint_type.go', import.meta.url),
      'utf8',
    );
    const backend = [
      ...source.matchAll(
        /^\s*EndpointType\w+\s+EndpointType\s*=\s*"([^"]+)"/gm,
      ),
    ].map((match) => match[1]);
    expect(
      CHANNEL_TEST_ENDPOINTS.map(({ value }) => value)
        .filter(Boolean)
        .sort(),
    ).toEqual(backend.sort());
  });
});
