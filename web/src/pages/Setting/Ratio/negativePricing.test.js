import { describe, expect, test } from 'bun:test';
import {
  getNegativePricingItems,
  isCompletePrice,
  PRICE_INPUT_PATTERN,
  PRICING_KEYS,
} from './negativePricing';

const t = (key) => key;
const inspect = (options, keys = PRICING_KEYS) =>
  getNegativePricingItems(options, keys, t);

describe('negative model pricing', () => {
  test('allows typing signs, but only complete finite prices may be saved', () => {
    for (const value of ['-', '-.', '-0.1', '-1', '-.5', '', '0', '2.']) {
      expect(PRICE_INPUT_PATTERN.test(value)).toBe(true);
    }
    for (const value of [
      '-',
      '-.',
      '',
      '.',
      'Infinity',
      'NaN',
      ' ',
      '9'.repeat(400),
    ]) {
      expect(isCompletePrice(value)).toBe(false);
    }
    for (const value of ['-1', '-.5', '-0', '0', '1.2', '-1e-8'])
      expect(isCompletePrice(value)).toBe(true);
    for (const value of ['-1e', '-1e-']) {
      expect(PRICE_INPUT_PATTERN.test(value)).toBe(true);
      expect(isCompletePrice(value)).toBe(false);
    }
  });

  test('fixed prices win over hidden ratios, tiered models are excluded', () => {
    expect(
      inspect({
        ModelPrice: { fixed: 2, credit: -0.1, expr: -2, zero: -0 },
        ModelRatio: { fixed: -1, credit: -1 },
        'billing_setting.billing_mode': { expr: 'tiered_expr' },
      }),
    ).toEqual([
      { name: 'credit', label: '模型固定价格', value: -0.1, unit: '$/次' },
    ]);
  });

  test('uses effective signs for input, output, cache, image, and audio', () => {
    const items = inspect({
      ModelRatio: { model: -1 },
      CompletionRatio: { model: -2 },
      CacheRatio: { model: 0.5 },
      CreateCacheRatio: { model: 1.25 },
      ImageRatio: { model: 2 },
      AudioRatio: { model: -3 },
      AudioCompletionRatio: { model: -4 },
    });
    expect(items.map(({ value }) => value)).toEqual([-2, -1, -2.5, -4, -24]);
  });

  test('locked output ratios reflect the backend, including an unsaved negative base', () => {
    expect(
      inspect({
        ModelRatio: { model: -1 },
        CompletionRatio: { model: -5 },
        CompletionRatioMeta: { model: { locked: true, ratio: 3 } },
      }).map(({ value }) => value),
    ).toEqual([-2, -6]);
  });

  test('validates JSON objects and rejects silent coercions and non-finite values', () => {
    for (const value of [
      '[]',
      'null',
      '{"model":"-1"}',
      '{"model":false}',
      '{"model":1e999}',
      '{',
    ]) {
      expect(() => inspect({ ModelPrice: value })).toThrow();
    }
    expect(inspect({ ModelPrice: '{"model":-0.1}' })[0].value).toBe(-0.1);
    expect(() => inspect({ ModelRatio: { model: 1e308 } })).toThrow();
    expect(() =>
      inspect({ ModelRatio: { model: 1 }, CompletionRatio: { model: -1e308 } }),
    ).toThrow();
  });

  test('audio output uses the default audio input multiplier when it is unset', () => {
    expect(
      inspect({
        ModelRatio: { model: 1 },
        AudioCompletionRatio: { model: -2 },
      }),
    ).toEqual([
      { name: 'model', label: '音频输出价格', value: -4, unit: '$/1M tokens' },
    ]);
  });

  test('unrelated settings do not prompt and all submitted models are inspected', () => {
    expect(
      inspect({ ModelPrice: { model: -1 } }, ['ExposeRatioEnabled']),
    ).toEqual([]);
    expect(
      inspect({ ModelPrice: { a: -1, b: -2 } }, ['ModelPrice']),
    ).toHaveLength(2);
  });
});
