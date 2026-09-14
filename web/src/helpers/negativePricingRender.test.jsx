import { expect, mock, test } from 'bun:test';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import i18next from 'i18next';

// These tests exercise pricing renderers without initializing browser-only UI
// dependencies such as Semi's animation player.
mock.module('@douyinfe/semi-ui', () => ({
  Modal: {},
  Tag: 'span',
  Typography: { Text: 'span' },
  Avatar: 'span',
  Toast: {},
  Pagination: 'span',
}));
mock.module('@lobehub/icons', () =>
  Object.fromEntries(
    (
      'OpenAI Claude Gemini Moonshot Zhipu Qwen DeepSeek Minimax Wenxin Spark ' +
      'Midjourney Hunyuan Cohere Cloudflare Ai360 Yi Jina Mistral XAI Ollama ' +
      'Doubao Suno Xinference OpenRouter Dify Coze SiliconCloud FastGPT Kling ' +
      'Jimeng Perplexity Replicate Poe Cerebras Vercel'
    )
      .split(' ')
      .map((name) => [name, 'span']),
  ),
);
const storage = new Map([
  ['quota_display_type', 'USD'],
  ['quota_per_unit', '500000'],
]);
globalThis.localStorage = { getItem: (key) => storage.get(key) ?? null };
globalThis.window = { matchMedia: () => ({ matches: false }) };
globalThis.React = React;
await i18next.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  keySeparator: false,
});
const {
  renderModelPriceSimple,
  renderModelPrice,
  renderAudioModelPrice,
  renderClaudeModelPrice,
} = await import('./render.jsx');
const { calculateModelPrice } = await import('./utils.jsx');

test('a fixed price of -1 renders as a credit, distinct from historical token pricing', () => {
  for (const currency of ['USD', 'TOKENS']) {
    storage.set('quota_display_type', currency);
    const opts = {
      model_price: -1,
      model_ratio: 2,
      group_ratio: 1,
      use_price: true,
      outputMode: 'segments',
    };
    const text = renderModelPriceSimple(opts)
      .map((segment) => segment.text)
      .join(' ');
    expect(text).toContain('$-1');
    expect(text).not.toContain('1M tokens');
    for (const renderer of [
      renderModelPrice,
      renderAudioModelPrice,
      renderClaudeModelPrice,
    ]) {
      expect(renderToStaticMarkup(renderer(opts))).toContain('-1');
      expect(renderToStaticMarkup(renderer(opts))).toContain('次');
    }
    const legacy = renderModelPriceSimple({ ...opts, use_price: undefined })
      .map((segment) => segment.text)
      .join(' ');
    expect(legacy).not.toContain('模型价格 $-1');
  }
});

test('model price tables retain negative signs through currency and token-unit formatting', () => {
  for (const currency of ['USD', 'CNY', 'CUSTOM']) {
    const result = calculateModelPrice({
      record: { quota_type: 0, model_ratio: -1, completion_ratio: -2 },
      selectedGroup: 'default',
      groupRatio: { default: 1 },
      tokenUnit: 'M',
      displayPrice: (price) => `$${price.toFixed(3)}`,
      currency,
    });
    expect(result.inputPrice).toContain('-2.0000');
    expect(result.completionPrice).toContain('4.0000');
    expect(result.completionPrice).not.toContain('-');
  }
});

test('text logs include signed audio input and output in the net total', () => {
  storage.set('quota_display_type', 'USD');
  const opts = {
    use_price: false,
    model_ratio: 1,
    completion_ratio: 1,
    group_ratio: 1,
    prompt_tokens: 10000000,
    completion_tokens: 5000000,
    audio_input_seperate_price: true,
    audio_input_token_count: 5000000,
    audio_input_price: -4,
    audio_output_token_count: 5000000,
    audio_output_price: -12,
  };
  const html = renderToStaticMarkup(renderModelPrice(opts));
  expect(html).toContain('-70');
  expect(html).toContain('音频输出');
  storage.set('quota_display_type', 'TOKENS');
  const tokens = renderToStaticMarkup(renderModelPrice(opts));
  expect(tokens).toContain('音频输入');
  expect(tokens).toContain('音频输出');
});
