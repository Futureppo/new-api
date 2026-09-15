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

const finiteNumber = (value) => {
  if (
    (typeof value !== 'number' && typeof value !== 'string') ||
    (typeof value === 'string' && value.trim() === '')
  ) {
    return null;
  }
  const number = Number(value);
  return Number.isFinite(number) ? number : null;
};

// Shared by price display and sorting, including the zero-multiplier case.
export const resolvePricingGroup = (
  record,
  selectedGroup = 'all',
  groupRatio = {},
) => {
  const selectedRatio = finiteNumber(groupRatio[selectedGroup]);
  if (selectedGroup !== 'all' && selectedRatio !== null) {
    return { usedGroup: selectedGroup, usedGroupRatio: selectedRatio };
  }

  let usedGroup = selectedGroup;
  let usedGroupRatio = null;
  for (const group of record.enable_groups || []) {
    const ratio = finiteNumber(groupRatio[group]);
    if (ratio !== null && (usedGroupRatio === null || ratio < usedGroupRatio)) {
      usedGroup = group;
      usedGroupRatio = ratio;
    }
  }
  return { usedGroup, usedGroupRatio: usedGroupRatio ?? 1 };
};

const inputPrice = (model) => {
  const ratio = finiteNumber(model.model_ratio);
  return ratio === null ? null : ratio * 2;
};

const PRICING_SORTS = {
  default: { label: (t) => t('默认排序') },
  input_price: {
    label: (t) => t('输入价格'),
    quotaType: 0,
    price: inputPrice,
  },
  output_price: {
    label: (t) => t('输出价格'),
    quotaType: 0,
    price: (model) => {
      const input = inputPrice(model);
      const completion = finiteNumber(model.completion_ratio);
      return input === null || completion === null ? null : input * completion;
    },
  },
  per_call_price: {
    label: (t) => t('单次价格'),
    quotaType: 1,
    price: (model) => finiteNumber(model.model_price),
  },
  model_name: { label: (t) => t('模型名称') },
};

export const getPricingSortOptions = (t) =>
  Object.entries(PRICING_SORTS).map(([value, config]) => ({
    value,
    label: config.label(t),
  }));

export const getModelPriceSortValue = (
  model,
  sortBy,
  selectedGroup,
  groupRatio,
) => {
  const config = PRICING_SORTS[sortBy];
  if (
    !config?.price ||
    model.quota_type !== config.quotaType ||
    model.billing_mode === 'tiered_expr'
  ) {
    return null;
  }
  const price = config.price(model);
  if (price === null) return null;
  const { usedGroupRatio } = resolvePricingGroup(
    model,
    selectedGroup,
    groupRatio,
  );
  return finiteNumber(price * usedGroupRatio);
};

const nameCollator = new Intl.Collator('en', {
  numeric: true,
  sensitivity: 'base',
});

export const sortPricingModels = (
  models,
  {
    sortBy = 'default',
    sortDirection = 'asc',
    selectedGroup = 'all',
    groupRatio = {},
  } = {},
) => {
  if (sortBy === 'default' || !Object.hasOwn(PRICING_SORTS, sortBy))
    return models;
  const direction = sortDirection === 'desc' ? -1 : 1;

  // Calculate each raw price once. Never sort the source array in place.
  return models
    .map((model, index) => ({
      model,
      index,
      price: getModelPriceSortValue(model, sortBy, selectedGroup, groupRatio),
    }))
    .sort((a, b) => {
      const byName = nameCollator.compare(
        a.model.model_name || '',
        b.model.model_name || '',
      );
      if (sortBy === 'model_name')
        return byName * direction || a.index - b.index;
      if (a.price === null && b.price !== null) return 1;
      if (a.price !== null && b.price === null) return -1;
      if (a.price !== null && b.price !== null && a.price !== b.price) {
        return (a.price < b.price ? -1 : 1) * direction;
      }
      return byName || a.index - b.index;
    })
    .map(({ model }) => model);
};
