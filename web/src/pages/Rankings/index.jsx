/* eslint-disable header/header -- Preserve the upstream copyright notice. */
/*
Copyright (C) 2023-2026 QuantumNous

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
import React, { useEffect, useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Button, Spin } from '@douyinfe/semi-ui';
import {
  BarChart3,
  PieChart,
  RefreshCw,
  Trophy,
  TrendingUp,
  TrendingDown,
} from 'lucide-react';
import { VChart } from '@visactor/react-vchart';
import { API, getLobeHubIcon } from '../../helpers';
import { useActualTheme } from '../../context/Theme';
import './rankings.css';

const PERIODS = [
  ['today', '今天'],
  ['week', '本周'],
  ['month', '本月'],
  ['year', '今年'],
];
const DESCRIPTIONS = {
  today: '最近 24 小时各模型的 Token 用量',
  week: '最近 7 天各模型的 Token 用量',
  month: '最近 30 天各模型的 Token 用量',
  year: '最近一年各模型的每周 Token 用量',
};
const COLORS = [
  '#1764ff',
  '#adcfff',
  '#13b5e8',
  '#69dcf2',
  '#ffc400',
  '#ff8c00',
  '#75d896',
  '#39c48c',
  '#ab8af0',
  '#7440d5',
  '#a1a1aa',
];
const CHART_OPTIONS = { mode: 'desktop-browser' };

export function formatTokens(value) {
  const number = Number(value) || 0;
  for (const [unit, divisor] of [
    ['T', 1e12],
    ['B', 1e9],
    ['M', 1e6],
    ['K', 1e3],
  ]) {
    if (Math.abs(number) >= divisor)
      return (number / divisor).toFixed(number / divisor >= 10 ? 1 : 2) + unit;
  }
  return number.toLocaleString();
}
const share = (value) => ((Number(value) || 0) * 100).toFixed(1) + '%';

function Growth({ row }) {
  const { t } = useTranslation();
  if (row.is_new)
    return <span className='rankings-growth up'>{t('新上榜')}</span>;
  const value = Number(row.growth_pct) || 0;
  return (
    <span
      className={
        'rankings-growth ' + (value > 0 ? 'up' : value < 0 ? 'down' : '')
      }
    >
      {value > 0 ? '↑' : value < 0 ? '↓' : ''}
      {Math.abs(value).toFixed(Math.abs(value) >= 100 ? 0 : 1)}%
    </span>
  );
}

function Entity({ row, vendor = false }) {
  const { t } = useTranslation();
  const name = vendor ? row.vendor : row.model_name;
  const vendorName = row.vendor === 'Unknown' ? t('未知') : row.vendor;
  return (
    <div className='rankings-entity'>
      <span className='rankings-entity-icon'>
        {getLobeHubIcon(row.vendor_icon || 'Layers', 28)}
      </span>
      <div className='rankings-entity-text'>
        <Link
          className={vendor ? '' : 'rankings-model-name'}
          to={
            '/pricing?' +
            (vendor ? 'vendor=' : 'search=') +
            encodeURIComponent(name)
          }
        >
          {vendor ? vendorName : name}
        </Link>
        {!vendor && (
          <Link
            className='rankings-vendor-name'
            to={
              '/pricing?vendor=' +
              encodeURIComponent(
                row.vendor === 'Unknown' ? 'unknown' : row.vendor,
              )
            }
          >
            {vendorName}
          </Link>
        )}
      </div>
    </div>
  );
}

function HistoryChart({ history, period, vendors = false }) {
  const { t, i18n } = useTranslation();
  const theme = useActualTheme();
  const spec = useMemo(() => {
    const field = vendors ? 'vendor' : 'model';
    const series = vendors ? history?.vendors : history?.models;
    const order = new Map((series || []).map((row, i) => [row.name, i]));
    const points = [...(history?.points || [])].sort(
      (a, b) =>
        a.ts.localeCompare(b.ts) ||
        (order.get(a[field]) ?? 999) - (order.get(b[field]) ?? 999),
    );
    const text = theme === 'dark' ? '#a1a1aa' : '#858994';
    const grid = theme === 'dark' ? '#383a40' : '#e5e7eb';
    return {
      type: 'bar',
      background: 'transparent',
      animation: false,
      color: COLORS,
      padding: { top: 20, right: 12, bottom: 8, left: 0 },
      data: [
        {
          id: 'history',
          values: points.map((row) => ({
            ...row,
            label: new Date(row.ts).toLocaleString(
              i18n.language,
              period === 'today'
                ? { hour: '2-digit', minute: '2-digit' }
                : { month: 'short', day: 'numeric' },
            ),
            [field]: row[field] === 'Others' ? t('其他') : row[field],
          })),
        },
      ],
      xField: 'label',
      yField: vendors ? 'share' : 'tokens',
      seriesField: field,
      stack: true,
      legends: { visible: false },
      axes: [
        {
          orient: 'bottom',
          label: {
            autoHide: true,
            autoLimit: true,
            style: { fill: text, fontSize: 12 },
          },
          tick: { visible: false },
          domainLine: { style: { stroke: grid } },
        },
        {
          orient: 'left',
          min: 0,
          ...(vendors ? { max: 1 } : {}),
          label: {
            formatMethod: (val) => (vendors ? share(val) : formatTokens(val)),
            style: { fill: text, fontSize: 12 },
          },
          domainLine: { visible: false },
          tick: { visible: false },
          grid: { visible: true, style: { lineDash: [4, 4], stroke: grid } },
        },
      ],
      tooltip: {
        mark: {
          content: [
            {
              key: (row) => row[field],
              value: (row) =>
                vendors ? share(row.share) : formatTokens(row.tokens),
            },
          ],
        },
        dimension: {
          title: { value: (row) => row.label },
          content: [
            {
              key: (row) => row[field],
              value: (row) =>
                vendors ? share(row.share) : formatTokens(row.tokens),
            },
          ],
        },
      },
    };
  }, [history, period, vendors, theme, t, i18n.language]);
  if (!history?.points?.length)
    return <div className='rankings-empty'>{t('暂无数据')}</div>;
  return (
    <div
      className='rankings-chart'
      role='img'
      aria-label={t(vendors ? '供应商份额趋势' : '模型用量趋势')}
    >
      <VChart
        key={theme + period + vendors}
        spec={spec}
        option={CHART_OPTIONS}
      />
    </div>
  );
}

function RankingTable({ rows, vendors = false }) {
  const { t } = useTranslation();
  return (
    <div className='rankings-table-scroll'>
      <table className='rankings-table'>
        <thead>
          <tr>
            <th>{t('排名')}</th>
            <th>{t(vendors ? '供应商' : '模型')}</th>
            <th>{t('占比')}</th>
            <th>Token</th>
            <th>{t('增长')}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={vendors ? row.vendor : row.model_name}>
              <td className='rankings-rank'>{row.rank}</td>
              <td>
                <Entity row={row} vendor={vendors} />
              </td>
              <td>
                <span className='rankings-share'>{share(row.share)}</span>
              </td>
              <td
                className='rankings-token-count'
                title={Number(row.total_tokens).toLocaleString()}
              >
                {formatTokens(row.total_tokens)}
              </td>
              <td>
                <Growth row={row} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {rows.length === 0 && (
        <div className='rankings-empty'>{t('暂无数据')}</div>
      )}
    </div>
  );
}

function ModelsSection({ data, period }) {
  const { t } = useTranslation();
  return (
    <section className='rankings-card'>
      <header className='rankings-card-header'>
        <div className='rankings-section-heading'>
          <span className='rankings-section-icon'>
            <BarChart3 size={23} />
          </span>
          <div>
            <h2>{t('热门模型')}</h2>
            <p>{t(DESCRIPTIONS[period])}</p>
          </div>
        </div>
        <div className='rankings-total'>
          <strong>{formatTokens(data.total_tokens)}</strong>
          <span>Token</span>
        </div>
      </header>
      <div className='rankings-model-overview'>
        <div className='rankings-trend'>
          <div className='rankings-chart-heading'>
            <span>{t('用量趋势')}</span>
            <span className='rankings-model-count'>
              {t('{{count}} 个模型', { count: data.models.length })}
            </span>
          </div>
          <HistoryChart history={data.models_history} period={period} />
        </div>
        <aside className='rankings-popular'>
          <div className='rankings-popular-heading'>
            <h3>
              <Trophy size={18} />
              {t('当前热门')}
            </h3>
            <span>{t('占比')}</span>
          </div>
          <ol>
            {data.models.slice(0, 5).map((row) => (
              <li key={row.model_name}>
                <span className='rankings-rank'>{row.rank}</span>
                <Entity row={row} />
                <div className='rankings-popular-stats'>
                  <strong>{share(row.share)}</strong>
                  <Growth row={row} />
                </div>
              </li>
            ))}
          </ol>
          {data.models.length === 0 && (
            <div className='rankings-empty'>{t('暂无数据')}</div>
          )}
        </aside>
      </div>
      <div className='rankings-table-section'>
        <header>
          <h3>
            <Trophy size={18} />
            {t('LLM 排行榜')}
          </h3>
          <p>{t('对比平台上最受欢迎的模型')}</p>
        </header>
        <RankingTable rows={data.models} />
      </div>
    </section>
  );
}

function VendorsSection({ data, period }) {
  const { t } = useTranslation();
  return (
    <section className='rankings-card'>
      <header className='rankings-card-header'>
        <div className='rankings-section-heading'>
          <span className='rankings-section-icon'>
            <PieChart size={23} />
          </span>
          <div>
            <h2>{t('市场份额')}</h2>
            <p>{t('按 Token 用量统计的供应商份额')}</p>
          </div>
        </div>
      </header>
      <div className='rankings-vendor-chart'>
        <HistoryChart
          history={data.vendor_share_history}
          period={period}
          vendors
        />
      </div>
      <div className='rankings-table-section'>
        <header>
          <h3>{t('供应商排行榜')}</h3>
        </header>
        <RankingTable rows={data.vendors} vendors />
      </div>
    </section>
  );
}

function Movers({ rows, down = false }) {
  const { t } = useTranslation();
  const Icon = down ? TrendingDown : TrendingUp;
  return (
    <section className='rankings-card rankings-movers'>
      <header>
        <h3>
          <Icon size={19} />
          {t(down ? '排名下降' : '排名上升')}
        </h3>
      </header>
      {rows.length === 0 ? (
        <div className='rankings-empty'>{t('暂无排名变化')}</div>
      ) : (
        <ul>
          {rows.map((row) => (
            <li key={row.model_name}>
              <Entity row={row} />
              <span className={'rankings-growth ' + (down ? 'down' : 'up')}>
                {down ? '↓' : '↑'}
                {Math.abs(row.rank_delta)}
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

export default function Rankings() {
  const { t } = useTranslation();
  const [params, setParams] = useSearchParams();
  const requested = params.get('period');
  const period = PERIODS.some(([id]) => id === requested) ? requested : 'week';
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [refresh, setRefresh] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError('');
    setData(null);
    API.get('/api/rankings', {
      params: { period },
      signal: controller.signal,
      disableDuplicate: true,
      skipErrorHandler: true,
    })
      .then(({ data: response }) => {
        if (controller.signal.aborted) return;
        if (!response.success) throw new Error(t('排行榜加载失败'));
        setData(response.data);
      })
      .catch(() => {
        if (!controller.signal.aborted) setError(t('排行榜加载失败'));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [period, refresh, t]);

  return (
    <main className='rankings-page'>
      <header className='rankings-hero'>
        <div className='rankings-title-row'>
          <h1>{t('排行榜')}</h1>
          <Button
            theme='borderless'
            icon={<RefreshCw size={17} />}
            aria-label={t('刷新')}
            onClick={() => setRefresh((value) => value + 1)}
            disabled={loading}
          />
        </div>
        <p>
          {t(
            '探索平台上使用最多的模型和增长中的供应商，数据基于实时用量更新。',
          )}
        </p>
        <div
          className='rankings-periods'
          role='tablist'
          aria-label={t('统计周期')}
        >
          {PERIODS.map(([id, label]) => (
            <button
              key={id}
              type='button'
              role='tab'
              aria-selected={period === id}
              onClick={() => setParams({ period: id })}
            >
              {t(label)}
            </button>
          ))}
        </div>
      </header>
      {loading ? (
        <div
          className='rankings-loading'
          role='status'
          aria-label={t('加载中')}
        >
          <Spin size='large' />
        </div>
      ) : error ? (
        <div className='rankings-error' role='alert'>
          <p>{error}</p>
          <Button onClick={() => setRefresh((value) => value + 1)}>
            {t('重试')}
          </Button>
        </div>
      ) : (
        data && (
          <>
            <ModelsSection data={data} period={period} />
            <VendorsSection data={data} period={period} />
            <div className='rankings-movement-grid'>
              <Movers rows={data.top_movers} />
              <Movers rows={data.top_droppers} down />
            </div>
          </>
        )
      )}
    </main>
  );
}
