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

const options = () =>
  getChannelTestEndpoints((key) => key).map(({ value }) => value);

describe('channel test endpoints', () => {
  test('exposes the full endpoint catalog without a channel filter', () => {
    expect(options()).toEqual(CHANNEL_TEST_ENDPOINTS.map(({ value }) => value));
    expect(options()[0]).toBe('');
    for (const value of [
      'openai-decisions',
      'moderations',
      'audio-speech',
      'audio-transcription',
      'batch-generation',
      'typesafe-systemone',
      'vertex-native',
      'cohere-chat',
      'cohere-rerank',
      'cohere-embeddings',
    ]) {
      expect(options()).toContain(value);
    }
    expect(channelTestSupportsStream(1, '')).toBe(true);
    expect(channelTestSupportsStream(1, 'openai-response')).toBe(true);
  });

  test('keeps stream capabilities separate from endpoint visibility', () => {
    expect(channelTestSupportsStream(71, '')).toBe(false);
    expect(channelTestSupportsStream(71, 'openai')).toBe(true);
    expect(channelTestSupportsStream(71, 'anthropic')).toBe(true);
    for (const endpoint of [
      '',
      'openai',
      'openai-response',
      'anthropic',
      'audio-speech',
    ]) {
      expect(channelTestSupportsStream(77, endpoint)).toBe(true);
    }
    for (const endpoint of [
      'typesafe-systemone',
      'jina-rerank',
      'image-generation',
      'openai-video',
      'embeddings',
    ]) {
      expect(channelTestSupportsStream(77, endpoint)).toBe(false);
    }
    expect(channelTestSupportsStream(77, 'openai-response-compact')).toBe(
      false,
    );
    for (const type of [1, 3, 20, 34, 41, 67, 71, 72, 77, '34', undefined]) {
      for (const endpoint of [
        'openai-decisions',
        'moderations',
        'audio-transcription',
        'batch-generation',
        'typesafe-systemone',
        'vertex-native',
      ]) {
        expect(channelTestSupportsStream(type, endpoint)).toBe(false);
      }
      expect(channelTestSupportsStream(type, 'unknown-endpoint')).toBe(false);
    }
    expect(channelTestSupportsStream(1, 'audio-speech')).toBe(false);
  });

  test('the displayed options cover every backend channel test endpoint', () => {
    const source = readFileSync(
      new URL('../../../constant/endpoint_type.go', import.meta.url),
      'utf8',
    );
    const backend = [
      ...source.matchAll(
        /^\s*EndpointType\w+\s+EndpointType\s*=\s*"([^"]+)"/gm,
      ),
    ].map((match) => match[1]);
    expect(options().filter(Boolean).sort()).toEqual(backend.sort());
  });
});

describe('OpenRouter channel test streams', () => {
  test('retains synchronous samples and streaming protocols', () => {
    expect(channelTestSupportsStream(20, 'openai')).toBe(true);
    expect(channelTestSupportsStream(20, 'anthropic')).toBe(true);
    expect(channelTestSupportsStream(20, 'openai-response')).toBe(true);
    for (const endpoint of [
      'audio-speech',
      'audio-transcription',
      'embeddings',
      'jina-rerank',
      'openai-response-compact',
      'openai-video',
    ]) {
      expect(channelTestSupportsStream(20, endpoint)).toBe(false);
    }
  });
});

describe('Vertex inference streams', () => {
  test('keeps native inference tests synchronous', () => {
    expect(channelTestSupportsStream(41, 'vertex-native')).toBe(false);
  });
});
