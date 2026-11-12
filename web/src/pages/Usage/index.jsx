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

import React, { useContext, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Banner,
  Button,
  Card,
  Empty,
  InputNumber,
  Modal,
  Select,
  Space,
  Spin,
  Table,
  Tabs,
  TabPane,
  Tag,
} from '@douyinfe/semi-ui';
import { Activity, Gauge, Users, Wallet, Zap } from 'lucide-react';
import { initVChartSemiTheme } from '@visactor/vchart-semi-theme';
import dayjs from 'dayjs';
import { API, getCurrencyConfig, isAdmin } from '../../helpers';
import { quotaToDisplayAmount } from '../../helpers/quota';
import { createSectionTitle, getTrendSpec } from '../../helpers/dashboard';
import { UserContext } from '../../context/User';
import DashboardHeader from '../../components/dashboard/DashboardHeader';
import StatsCards from '../../components/dashboard/StatsCards';
import ChartsPanel from '../../components/dashboard/ChartsPanel';
import {
  CARD_PROPS,
  CHART_CONFIG,
  FLEX_CENTER_GAP2,
} from '../../constants/dashboard.constants';

const number = (value) => Number(value || 0).toLocaleString();
const PERIOD_LABELS = {
  '24h': '最近 24 小时',
  '7d': '最近 7 天',
  '30d': '最近 30 天',
};
const amount = (quota) => {
  const { type, symbol } = getCurrencyConfig();
  const value = quotaToDisplayAmount(quota);
  return type === 'TOKENS'
    ? number(value)
    : `${symbol}${value.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 4 })}`;
};

// The API returns bounded rankings. Preserve the remainder in pie charts so a
// top-20 ranking is never mistaken for the whole site's usage distribution.
const distribution = (rows, field, total, otherLabel) => {
  const values = rows
    .map((row) => ({ name: row.model_name, value: Number(row[field] || 0) }))
    .filter((row) => row.value > 0);
  const remainder = total - values.reduce((sum, row) => sum + row.value, 0);
  if (remainder > 0) values.push({ name: otherLabel, value: remainder });
  return values;
};

const pieSpec = (values, formatter) => ({
  type: 'pie',
  background: 'transparent',
  animation: false,
  data: [{ id: 'distribution', values }],
  outerRadius: 0.8,
  innerRadius: 0.5,
  valueField: 'value',
  categoryField: 'name',
  legends: {
    visible: true,
    orient: 'bottom',
    maxRow: 2,
    pager: { type: 'scrollbar' },
  },
  tooltip: {
    mark: {
      title: { value: (row) => row.name },
      content: [
        { key: (row) => row.name, value: (row) => formatter(row.value) },
      ],
    },
  },
});

const barSpec = (values, formatter) => ({
  type: 'bar',
  background: 'transparent',
  animation: false,
  data: [{ id: 'ranking', values }],
  xField: 'name',
  yField: 'value',
  axes: [
    { orient: 'bottom', label: { autoHide: true } },
    { orient: 'left', min: 0 },
  ],
  tooltip: {
    mark: {
      content: [
        { key: (row) => row.name, value: (row) => formatter(row.value) },
      ],
    },
  },
});

export default function Usage() {
  const { t } = useTranslation();
  const [userState, userDispatch] = useContext(UserContext);
  const admin = isAdmin();
  const [period, setPeriod] = useState('24h');
  const [userID, setUserID] = useState(0);
  const [section, setSection] = useState('models');
  const [chartTab, setChartTab] = useState('2');
  const [refresh, setRefresh] = useState(0);
  const [filterVisible, setFilterVisible] = useState(false);
  const [draftPeriod, setDraftPeriod] = useState('24h');
  const [draftUser, setDraftUser] = useState();
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    initVChartSemiTheme({ isWatchingThemeSwitch: true });
  }, []);
  useEffect(() => {
    let active = true;
    API.get('/api/user/self', { skipErrorHandler: true })
      .then(({ data: response }) => {
        if (active && response.success)
          userDispatch({ type: 'login', payload: response.data });
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, [refresh, userDispatch]);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setData(null);
    setError('');
    API.get(
      admin
        ? '/api/enhancements/dashboard/usage-stats'
        : '/api/data/usage/self',
      {
        params: admin ? { period, user_id: userID } : { period },
        signal: controller.signal,
        disableDuplicate: true,
        skipErrorHandler: true,
      },
    )
      .then(({ data: response }) => {
        if (controller.signal.aborted) return;
        if (!response.success)
          throw new Error(response.message || t('加载失败'));
        setData(response.data);
      })
      .catch((err) => {
        if (!controller.signal.aborted) setError(err.message || t('加载失败'));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [admin, period, userID, refresh, t]);

  const totals = data?.totals || {};
  const minutes = data ? Math.max(1, (data.generated_at - data.start) / 60) : 1;
  const statItem = (title, value, Icon, color, trendData = []) => ({
    title: t(title),
    value,
    icon: <Icon size={16} />,
    avatarColor: color,
    trendData,
    trendColor: '#6366f1',
  });
  const groups = [
    {
      title: createSectionTitle(Wallet, t('账户数据')),
      color: 'bg-blue-50',
      items: [
        statItem('当前余额', amount(userState.user?.quota), Wallet, 'blue'),
        statItem(
          '历史消耗',
          amount(userState.user?.used_quota),
          Wallet,
          'purple',
        ),
      ],
    },
    {
      title: createSectionTitle(Activity, t('使用统计')),
      color: 'bg-green-50',
      items: [
        statItem(
          '调用次数',
          number(totals.requests),
          Activity,
          'green',
          data?.trend.map((r) => r.requests),
        ),
        statItem('活跃用户', number(totals.active_users), Users, 'cyan'),
      ],
    },
    {
      title: createSectionTitle(Zap, t('资源消耗')),
      color: 'bg-yellow-50',
      items: [
        statItem(
          '统计额度',
          amount(totals.quota),
          Zap,
          'yellow',
          data?.trend.map((r) => r.quota),
        ),
        statItem(
          '统计Tokens',
          number(totals.token_used),
          Zap,
          'pink',
          data?.trend.map((r) => r.token_used),
        ),
      ],
    },
    {
      title: createSectionTitle(Gauge, t('性能指标')),
      color: 'bg-indigo-50',
      items: [
        statItem(
          '平均RPM',
          (Number(totals.requests || 0) / minutes).toFixed(2),
          Gauge,
          'indigo',
        ),
        statItem(
          '平均TPM',
          (Number(totals.token_used || 0) / minutes).toFixed(2),
          Gauge,
          'orange',
        ),
      ],
    },
  ];

  const chartTabs = useMemo(() => {
    if (!data) return [];
    return [
      {
        key: '1',
        label: '消耗分布',
        spec: pieSpec(
          distribution(data.models, 'quota', data.totals.quota, t('其他')),
          amount,
        ),
      },
      {
        key: '2',
        label: '调用趋势',
        spec: {
          type: 'line',
          background: 'transparent',
          animation: false,
          data: [
            {
              id: 'trend',
              values: data.trend.map((row) => ({
                time: dayjs.unix(row.created_at).format('MM-DD HH:mm'),
                count: row.requests,
              })),
            },
          ],
          xField: 'time',
          yField: 'count',
          point: { visible: false },
          axes: [
            { orient: 'bottom', label: { autoHide: true, autoRotate: false } },
            { orient: 'left', min: 0 },
          ],
          tooltip: {
            mark: {
              title: { value: (row) => row.time },
              content: [
                { key: () => t('调用次数'), value: (row) => number(row.count) },
              ],
            },
          },
        },
      },
      {
        key: '3',
        label: '调用次数分布',
        spec: pieSpec(
          distribution(
            data.models,
            'requests',
            data.totals.requests,
            t('其他'),
          ),
          number,
        ),
      },
      {
        key: '4',
        label: '模型调用次数',
        spec: barSpec(
          [...data.models]
            .sort((a, b) => b.requests - a.requests)
            .map((row) => ({ name: row.model_name, value: row.requests })),
          number,
        ),
      },
    ];
  }, [data, t]);
  const columns = [
    {
      title: t('调用次数'),
      dataIndex: 'requests',
      render: number,
      align: 'right',
      sorter: (a, b) => a.requests - b.requests,
    },
    {
      title: t('总 Token'),
      dataIndex: 'token_used',
      render: number,
      align: 'right',
      sorter: (a, b) => a.token_used - b.token_used,
    },
    {
      title: t('消耗额度'),
      dataIndex: 'quota',
      render: amount,
      align: 'right',
      sorter: (a, b) => a.quota - b.quota,
    },
  ];
  const openFilters = () => {
    setDraftPeriod(period);
    setDraftUser(userID || undefined);
    setFilterVisible(true);
  };
  const selectUser = (id) => {
    setUserID(id);
    setSection('models');
  };
  const validUser =
    draftUser == null ||
    draftUser === '' ||
    (Number.isSafeInteger(draftUser) && draftUser >= 0);

  return (
    <main className='mx-auto max-w-7xl px-3 md:px-6 pt-24 pb-8'>
      <DashboardHeader
        getGreeting={t('用量统计')}
        greetingVisible
        showSearchModal={openFilters}
        refresh={() => setRefresh((n) => n + 1)}
        loading={loading}
        t={t}
      />
      <div className='mb-4 flex flex-wrap items-center justify-between gap-3'>
        <Tabs type='line' activeKey={section} onChange={setSection}>
          <TabPane itemKey='models' tab={t('使用统计')} />
          {admin && <TabPane itemKey='users' tab={t('用户统计')} />}
        </Tabs>
        <Space wrap>
          <Button onClick={openFilters}>{t(PERIOD_LABELS[period])}</Button>
          {admin && (
            <Tag>{userID ? `${t('用户 ID')} ${userID}` : t('全部用户')}</Tag>
          )}
          {admin && userID > 0 && (
            <Button onClick={() => selectUser(0)}>{t('全部用户')}</Button>
          )}
        </Space>
      </div>
      {data && (
        <div className='mb-4 text-xs text-semi-color-text-2'>
          {dayjs.unix(data.start).format('YYYY-MM-DD HH:mm')} –{' '}
          {dayjs.unix(data.generated_at).format('YYYY-MM-DD HH:mm')} ·{' '}
          {t('数据更新于')} {dayjs.unix(data.generated_at).format('HH:mm:ss')}
        </div>
      )}
      {error && (
        <Banner
          className='mb-4'
          type='danger'
          description={error}
          closeIcon={null}
        />
      )}
      {data && !data.aggregation_enabled && (
        <Banner
          className='mb-4'
          type='warning'
          closeIcon={null}
          description={t('用量汇总已关闭，当前仅展示此前保存的数据。')}
        />
      )}
      <StatsCards
        groupedStatsData={groups}
        loading={loading}
        getTrendSpec={getTrendSpec}
        CARD_PROPS={CARD_PROPS}
        CHART_CONFIG={CHART_CONFIG}
      />
      {loading && (
        <div className='flex justify-center py-16'>
          <Spin size='large' />
        </div>
      )}
      {!loading &&
        data &&
        (data.totals.requests === 0 ? (
          <Empty title={t('当前范围暂无用量数据')} />
        ) : section === 'models' ? (
          <div className='flex flex-col gap-4'>
            <ChartsPanel
              activeChartTab={chartTab}
              setActiveChartTab={setChartTab}
              chartTabs={chartTabs}
              CARD_PROPS={CARD_PROPS}
              CHART_CONFIG={CHART_CONFIG}
              FLEX_CENTER_GAP2={FLEX_CENTER_GAP2}
              t={t}
            />
            <Card
              {...CARD_PROPS}
              className='!rounded-2xl'
              title={t('模型消耗排行（前 20）')}
            >
              <div className='overflow-x-auto'>
                <Table
                  rowKey='model_name'
                  size='small'
                  pagination={false}
                  style={{ minWidth: 640 }}
                  dataSource={data.models}
                  columns={[
                    { title: t('模型'), dataIndex: 'model_name', width: 240 },
                    ...columns,
                  ]}
                />
              </div>
            </Card>
          </div>
        ) : (
          admin && (
            <div className='flex flex-col gap-4'>
              <ChartsPanel
                title='用户统计'
                activeChartTab='users'
                chartTabs={[
                  {
                    key: 'users',
                    label: '用户消耗排行',
                    spec: barSpec(
                      data.users.map((row) => ({
                        name: `${row.username || t('用户')} #${row.user_id}`,
                        value: quotaToDisplayAmount(row.quota),
                      })),
                      number,
                    ),
                  },
                ]}
                CARD_PROPS={CARD_PROPS}
                CHART_CONFIG={CHART_CONFIG}
                FLEX_CENTER_GAP2={FLEX_CENTER_GAP2}
                t={t}
              />
              <Card
                {...CARD_PROPS}
                className='!rounded-2xl'
                title={t('用户消耗排行（前 20）')}
              >
                <div className='overflow-x-auto'>
                  <Table
                    rowKey='user_id'
                    size='small'
                    pagination={false}
                    style={{ minWidth: 640 }}
                    dataSource={data.users}
                    columns={[
                      {
                        title: t('用户'),
                        dataIndex: 'username',
                        width: 240,
                        render: (name, row) => (
                          <Button
                            theme='borderless'
                            onClick={() => selectUser(row.user_id)}
                          >
                            {name || t('用户')} #{row.user_id}
                          </Button>
                        ),
                      },
                      ...columns,
                    ]}
                  />
                </div>
              </Card>
            </div>
          )
        ))}
      <Modal
        title={t('查询条件')}
        visible={filterVisible}
        onCancel={() => setFilterVisible(false)}
        okText={t('查询')}
        okButtonProps={{ disabled: !validUser, 'aria-label': t('查询') }}
        onOk={() => {
          setPeriod(draftPeriod);
          setUserID(Number(draftUser) || 0);
          setFilterVisible(false);
        }}
      >
        <div className='flex flex-col gap-4 py-3'>
          <span id='usage-period-label'>{t('统计周期')}</span>
          <Select
            aria-labelledby='usage-period-label'
            value={draftPeriod}
            onSelect={setDraftPeriod}
            onChange={setDraftPeriod}
            optionList={Object.entries(PERIOD_LABELS).map(([value, label]) => ({
              value,
              label: t(label),
            }))}
          />
          {admin && (
            <>
              <span>{t('筛选用户 ID')}</span>
              <InputNumber
                aria-label={t('筛选用户 ID')}
                placeholder={t('全部用户')}
                min={0}
                precision={0}
                value={draftUser}
                onChange={setDraftUser}
                style={{ width: '100%' }}
              />
            </>
          )}
        </div>
      </Modal>
    </main>
  );
}
