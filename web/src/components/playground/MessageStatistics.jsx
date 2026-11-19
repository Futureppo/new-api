import React from 'react';
import { useTranslation } from 'react-i18next';

export default function MessageStatistics({ statistics }) {
  const { t, i18n } = useTranslation();
  if (!statistics) return null;
  const number = new Intl.NumberFormat(i18n.resolvedLanguage || i18n.language, {
    maximumFractionDigits: 1,
  });
  const time = (ms) =>
    ms >= 1000 ? `${number.format(ms / 1000)} s` : `${number.format(ms)} ms`;
  const metrics = [
    [t('输入 Token'), statistics.input, number.format],
    [t('输出 Token'), statistics.output, number.format],
    [
      t('输出速度'),
      statistics.tokensPerSecond,
      (n) => `${number.format(n)} token/s`,
    ],
    [t('缓存命中 Token'), statistics.cached, number.format],
    [
      t('缓存命中率'),
      statistics.cacheRate,
      (n) => `${number.format(n * 100)}%`,
    ],
    [t('首字延迟'), statistics.firstMs, time],
    [t('完成耗时'), statistics.durationMs, time],
  ];
  return (
    <dl className='mt-2 flex flex-wrap gap-x-4 gap-y-1 border-t border-[var(--semi-color-border)] pt-2 text-xs text-[var(--semi-color-text-2)] tabular-nums'>
      {metrics.map(([label, value, format]) => (
        <div key={label} className='inline-flex items-baseline gap-1.5'>
          <dt>{label}</dt>
          <dd className='m-0 text-[var(--semi-color-text-1)]'>
            {Number.isFinite(value) ? format(value) : '—'}
          </dd>
        </div>
      ))}
    </dl>
  );
}
