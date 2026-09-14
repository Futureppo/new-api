import { expect, mock, test } from 'bun:test';

mock.module('../../../../helpers', () => ({
  API: {},
  showError() {},
  showSuccess() {},
}));
mock.module('../components/confirmNegativePricing', () => ({
  confirmNegativePricing: async () => true,
}));
const { serializeModel } = await import('./useModelPricingEditorState');
const serialize = (values) =>
  serializeModel(
    {
      name: 'test-model',
      billingMode: 'per-token',
      rawRatios: {},
      ...values,
    },
    (key) => key,
  );

test('serializes signed prices, zero, tiny prices, and mixed signs without losing them', () => {
  expect(
    serialize({ billingMode: 'per-request', fixedPrice: '-1' }).ModelPrice,
  ).toBe(-1);
  expect(
    serialize({ billingMode: 'per-request', fixedPrice: '-1e-15' }).ModelPrice,
  ).toBe(-1e-15);
  expect(
    Object.is(
      serialize({ billingMode: 'per-request', fixedPrice: '-0' }).ModelPrice,
      -0,
    ),
  ).toBe(false);
  const mixed = serialize({
    inputPrice: '-2',
    completionPrice: '4',
    cachePrice: '-0.5',
    audioInputPrice: '6',
    audioOutputPrice: '-12',
  });
  expect(mixed).toMatchObject({
    ModelRatio: -1,
    CompletionRatio: -2,
    CacheRatio: 0.25,
    AudioRatio: -3,
    AudioCompletionRatio: -2,
  });
  expect(
    serialize({
      inputPrice: '0',
      completionPrice: '0',
      audioInputPrice: '0',
      audioOutputPrice: '0',
    }),
  ).toMatchObject({
    ModelRatio: 0,
    CompletionRatio: 0,
    AudioRatio: 0,
    AudioCompletionRatio: 0,
  });
});

test('rejects incomplete numbers and combinations that cannot be represented', () => {
  for (const fixedPrice of ['-', '-.', 'NaN', 'Infinity', '1e999']) {
    expect(() =>
      serialize({ billingMode: 'per-request', fixedPrice }),
    ).toThrow();
  }
  for (const values of [
    { inputPrice: '0', completionPrice: '-1' },
    { inputPrice: '1', audioInputPrice: '0', audioOutputPrice: '-1' },
    { inputPrice: '1e-308', completionPrice: '-1e308' },
    { inputPrice: '1e308', completionPrice: '-1e-308' },
    { inputPrice: '5e-324' },
  ])
    expect(() => serialize(values)).toThrow();
});

test('inactive pricing mode fields do not affect saving', () => {
  expect(
    serialize({ billingMode: 'per-request', fixedPrice: '1', inputPrice: '-.' })
      .ModelPrice,
  ).toBe(1);
  expect(serialize({ inputPrice: '1', fixedPrice: '-.' }).ModelRatio).toBe(0.5);
});
