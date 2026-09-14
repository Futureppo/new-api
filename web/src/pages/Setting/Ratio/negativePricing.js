export const PRICING_KEYS = [
  'ModelPrice',
  'ModelRatio',
  'CompletionRatio',
  'CacheRatio',
  'CreateCacheRatio',
  'ImageRatio',
  'AudioRatio',
  'AudioCompletionRatio',
];

export const PRICE_INPUT_PATTERN = /^-?((\d+(\.\d*)?|\.\d*)([eE][+-]?\d*)?)?$/;
export const isCompletePrice = (value) =>
  /^-?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$/.test(String(value)) &&
  Number.isFinite(Number(value));

export function parsePricingMap(value, key, t) {
  const parsed =
    typeof value === 'string' ? JSON.parse(value || '{}') : (value ?? {});
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error(
      t('配置 {{key}} 必须是模型名称到有限数字的 JSON 对象', { key }),
    );
  }
  if (
    PRICING_KEYS.includes(key) &&
    Object.values(parsed).some(
      (price) => typeof price !== 'number' || !Number.isFinite(price),
    )
  ) {
    throw new Error(
      t('配置 {{key}} 必须是模型名称到有限数字的 JSON 对象', { key }),
    );
  }
  return parsed;
}

// Examine effective prices, not the sign of a multiplier in isolation.
export function getNegativePricingItems(options, submittedKeys, t) {
  const changedKeys = submittedKeys.filter((key) => PRICING_KEYS.includes(key));
  if (!changedKeys.length) return [];
  const maps = Object.fromEntries(
    PRICING_KEYS.map((key) => [key, parsePricingMap(options[key], key, t)]),
  );
  const modes = parsePricingMap(
    options['billing_setting.billing_mode'],
    'billing_mode',
    t,
  );
  const completionMeta = parsePricingMap(
    options.CompletionRatioMeta,
    'CompletionRatioMeta',
    t,
  );
  const names = new Set(changedKeys.flatMap((key) => Object.keys(maps[key])));
  const items = [];
  const add = (name, label, value, unit = '$/1M tokens') => {
    if (Number.isFinite(value) && value < 0)
      items.push({ name, label, value, unit });
  };
  for (const name of [...names].sort()) {
    if (modes[name] === 'tiered_expr') continue;
    if (Object.hasOwn(maps.ModelPrice, name)) {
      add(name, t('模型固定价格'), maps.ModelPrice[name], '$/' + t('次'));
      continue;
    }
    const multiply = (...values) => {
      if (values.some((value) => value === undefined)) return undefined;
      const value = values.reduce((total, factor) => total * factor, 1);
      if (!Number.isFinite(value)) {
        throw new Error(
          t('模型 {{name}} 的价格必须是完整的有限数字', { name }),
        );
      }
      return value;
    };
    const base = multiply(maps.ModelRatio[name], 2);
    add(name, t('输入价格'), base);
    const completionRatio = completionMeta[name]?.locked
      ? completionMeta[name].ratio
      : maps.CompletionRatio[name];
    for (const [key, label, ratio] of [
      ['CompletionRatio', t('输出价格'), completionRatio],
      ['CacheRatio', t('缓存读取价格'), maps.CacheRatio[name]],
      ['CreateCacheRatio', t('缓存创建价格'), maps.CreateCacheRatio[name]],
      ['ImageRatio', t('图片输入价格'), maps.ImageRatio[name]],
      ['AudioRatio', t('音频输入价格'), maps.AudioRatio[name]],
    ]) {
      if (base === undefined) add(name, `${label} (${key})`, ratio, t('倍率'));
      else add(name, label, multiply(base, ratio));
    }
    const audioBase = multiply(base, maps.AudioRatio[name] ?? 1);
    if (Number.isFinite(audioBase)) {
      add(
        name,
        t('音频输出价格'),
        multiply(audioBase, maps.AudioCompletionRatio[name]),
      );
    } else {
      add(
        name,
        'AudioCompletionRatio',
        maps.AudioCompletionRatio[name],
        t('倍率'),
      );
    }
  }
  return items;
}
