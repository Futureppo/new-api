import { expect, test } from 'bun:test';
import {
  playgroundTemplate,
  protocolRequest,
  isPlaygroundChat,
} from './playgroundEndpoints';
import { playgroundUsage, replyStatistics } from './playgroundResponse';
import {
  channelTestSupportsStream,
  getChannelTestEndpoints,
} from './channelTestEndpoints';

test('Gemini Decisions uses official named questions at both entrances', () => {
  for (const endpoint of ['gemini-decisions', 'openai-decisions']) {
    const body = playgroundTemplate(endpoint, true, '', 'gemini');
    expect(body).toEqual({
      input: 'Hello',
      questions: {
        greeting: { type: 'noul', instructions: 'Is this a greeting?' },
      },
    });
    expect(body.stream).toBeUndefined();
    expect(isPlaygroundChat(endpoint)).toBe(false);
    expect(channelTestSupportsStream(24, endpoint)).toBe(false);
    expect(
      protocolRequest(
        endpoint,
        'default',
        'gemini-decision-1',
        false,
        true,
        body,
      ).payload.body,
    ).toEqual(body);
  }
  expect(
    getChannelTestEndpoints((s) => s).find(
      (e) => e.value === 'gemini-decisions',
    ).label,
  ).toContain('/v1beta/decisions');
  expect(playgroundTemplate('openai-decisions').questions).toBeArray();
  expect(playgroundTemplate('openai-decisions', false, '', 'named').state).toBe(
    'Hello',
  );
});

test('Gemini Decisions statistics preserve reported zero output', () => {
  const usage = playgroundUsage({
    usage: {
      total_input_tokens: 123,
      total_output_tokens: 0,
      total_tokens: 123,
    },
  });
  expect(usage.input).toBe(123);
  expect(usage.output).toBe(0);
  expect(replyStatistics(usage, 0, 100, 50, 100).output).toBe(0);
  expect(
    playgroundUsage({
      usage: { total_input_tokens: 0, total_output_tokens: 0 },
    }).input,
  ).toBe(0);
  expect(
    playgroundUsage({
      usage: { total_input_tokens: '123', total_output_tokens: -1 },
    }).output,
  ).toBeUndefined();
});
