import { describe, expect, test } from 'bun:test';
import {
  getModelPriceSortValue,
  resolvePricingGroup,
  sortPricingModels,
} from './modelPricing';

const token = (model_name, model_ratio, extra = {}) => ({
  model_name,
  quota_type: 0,
  model_ratio,
  completion_ratio: 2,
  enable_groups: ['regular', 'discount'],
  ...extra,
});
const call = (model_name, model_price, extra = {}) => ({
  model_name,
  quota_type: 1,
  model_price,
  enable_groups: ['regular'],
  ...extra,
});
const names = (models, options) =>
  sortPricingModels(models, options).map((model) => model.model_name);

describe('pricing sort', () => {
  test('sorts signed prices without rounding or dropping zero', () => {
    const models = [
      token('b', 0.000002),
      token('z', 0),
      token('c', 0.000001),
      token('a', -1),
    ];
    expect(names(models, { sortBy: 'input_price' })).toEqual([
      'a',
      'z',
      'c',
      'b',
    ]);
    expect(
      names(models, { sortBy: 'input_price', sortDirection: 'desc' }),
    ).toEqual(['b', 'c', 'z', 'a']);
    expect(models.map((m) => m.model_name)).toEqual(['b', 'z', 'c', 'a']);
  });

  test('output price uses the completion ratio, including explicit zero', () => {
    const models = [
      token('high-input', 5, { completion_ratio: 0.1 }),
      token('high-output', 1, { completion_ratio: 10 }),
      token('free-output', 3, { completion_ratio: 0 }),
    ];
    expect(names(models, { sortBy: 'output_price' })).toEqual([
      'free-output',
      'high-input',
      'high-output',
    ]);
    expect(
      names(models, { sortBy: 'output_price', sortDirection: 'desc' }),
    ).toEqual(['high-output', 'high-input', 'free-output']);
  });

  test('per-request prices only compare per-request models', () => {
    const models = [
      call('paid', '0.002'),
      token('token', -100),
      call('credit', -1),
      call('free', 0),
    ];
    expect(names(models, { sortBy: 'per_call_price' })).toEqual([
      'credit',
      'free',
      'paid',
      'token',
    ]);
    expect(
      names(models, { sortBy: 'per_call_price', sortDirection: 'desc' }),
    ).toEqual(['paid', 'free', 'credit', 'token']);
  });

  test('all groups use each model lowest multiplier and a selected group reorders prices', () => {
    const models = [
      token('a', 1, { enable_groups: ['regular'] }),
      token('b', 2),
    ];
    const groupRatio = { regular: 1, discount: 0.1 };
    expect(names(models, { sortBy: 'input_price', groupRatio })).toEqual([
      'b',
      'a',
    ]);
    expect(
      names(models, {
        sortBy: 'input_price',
        groupRatio,
        selectedGroup: 'regular',
      }),
    ).toEqual(['a', 'b']);
    expect(
      getModelPriceSortValue(models[1], 'output_price', 'discount', groupRatio),
    ).toBe(0.8);
  });

  test.each(['asc', 'desc'])(
    'incompatible, missing, invalid and dynamic prices always go last (%s)',
    (sortDirection) => {
      const models = [
        token('z-dynamic', -50, {
          billing_mode: 'tiered_expr',
          billing_expr: 'tier("base", p * 2)',
        }),
        token('b-missing', undefined),
        call('a-per-call', -100),
        token('c-invalid', NaN),
        token('d-empty', ''),
        token('e-null', null),
        token('f-infinite', Infinity),
        token('g-overflow', Number.MAX_VALUE),
        token('h-boolean', false),
        token('free', 0),
        token('paid', 2),
      ];
      const result = names(models, { sortBy: 'input_price', sortDirection });
      expect(result.slice(0, 2)).toEqual(
        sortDirection === 'asc' ? ['free', 'paid'] : ['paid', 'free'],
      );
      expect(result.slice(2)).toEqual([
        'a-per-call',
        'b-missing',
        'c-invalid',
        'd-empty',
        'e-null',
        'f-infinite',
        'g-overflow',
        'h-boolean',
        'z-dynamic',
      ]);
      expect(
        getModelPriceSortValue(
          token('missing-output', 1, { completion_ratio: null }),
          'output_price',
        ),
      ).toBeNull();
      expect(
        getModelPriceSortValue(
          call('dynamic-call', 0, { billing_mode: 'tiered_expr' }),
          'per_call_price',
        ),
      ).toBeNull();
    },
  );

  test('name sorting is natural, case insensitive and stable for equal names', () => {
    const models = [
      token('model-10', 1),
      token('Model-2', 1),
      token('model-2', 1),
    ];
    expect(names(models, { sortBy: 'model_name' })).toEqual([
      'Model-2',
      'model-2',
      'model-10',
    ]);
    expect(
      names(models, { sortBy: 'model_name', sortDirection: 'desc' }),
    ).toEqual(['model-10', 'Model-2', 'model-2']);
    expect(
      names(models, { sortBy: 'input_price', sortDirection: 'desc' }),
    ).toEqual(['Model-2', 'model-2', 'model-10']);
  });

  test('default order is preserved and sorting precedes pagination', () => {
    const models = Array.from({ length: 45 }, (_, index) =>
      token(`m-${index}`, 45 - index),
    );
    expect(sortPricingModels(models)).toBe(models);
    expect(sortPricingModels(models, { sortBy: 'future-sort' })).toBe(models);
    const sorted = sortPricingModels(models, { sortBy: 'input_price' });
    expect(sorted.slice(0, 20).map((m) => m.model_ratio)).toEqual(
      Array.from({ length: 20 }, (_, i) => i + 1),
    );
    expect(sorted.slice(20, 40)[0].model_ratio).toBe(21);
    expect(sorted[0]).toBe(models[44]);
  });
});

describe('shared pricing group', () => {
  test('uses explicit and minimum zero multipliers', () => {
    const model = token('free', 5);
    const groupRatio = { regular: 1, discount: 0, all: 10 };
    expect(resolvePricingGroup(model, 'all', groupRatio)).toEqual({
      usedGroup: 'discount',
      usedGroupRatio: 0,
    });
    expect(
      resolvePricingGroup(model, 'discount', groupRatio).usedGroupRatio,
    ).toBe(0);
    expect(
      getModelPriceSortValue(model, 'input_price', 'all', groupRatio),
    ).toBe(0);
  });

  test('falls back to valid available groups and then to one', () => {
    const model = token('fallback', 1);
    expect(
      resolvePricingGroup(model, 'unknown', { regular: 2, discount: 0.5 })
        .usedGroupRatio,
    ).toBe(0.5);
    expect(
      resolvePricingGroup(model, 'all', { regular: null, discount: Infinity })
        .usedGroupRatio,
    ).toBe(1);
    expect(resolvePricingGroup({}, 'all', { unrelated: 0 })).toEqual({
      usedGroup: 'all',
      usedGroupRatio: 1,
    });
  });
});
