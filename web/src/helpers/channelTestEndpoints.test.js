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
    expect(options(77)).toEqual([
      '',
      'openai',
      'openai-response',
      'anthropic',
      'jina-rerank',
      'image-generation',
      'openai-video',
      'embeddings',
      'audio-speech',
    ]);
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

describe('OpenRouter channel test endpoints', () => {
  test('exposes supported inference endpoints and stream choices', () => {
    expect(options(20)).toEqual([
      '',
      'openai',
      'openai-response',
      'anthropic',
      'jina-rerank',
      'image-generation',
      'embeddings',
      'audio-speech',
      'audio-transcription',
    ]);
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

describe('Vertex inference endpoints', () => {
  test('offers native inference and only supported compatibility protocols', () => {
    expect(options(41)[0]).toBe('');
    expect(options(41)).toContain('vertex-native');
    expect(options(41)).toContain('embeddings');
    expect(options(41)).toContain('audio-speech');
    expect(options(41)).not.toContain('openai-response-compact');
    expect(options(41)).not.toContain('jina-rerank');
    expect(options(1)).not.toContain('vertex-native');
    expect(channelTestSupportsStream(41, 'vertex-native')).toBe(false);
  });
});
