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

import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Banner,
  Button,
  Card,
  Empty,
  InputNumber,
  Select,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { VChart } from '@visactor/react-vchart';
import { RefreshCw } from 'lucide-react';
import dayjs from 'dayjs';
import { API, getCurrencyConfig } from '../../../helpers';
import { quotaToDisplayAmount } from '../../../helpers/quota';

const { Text } = Typography;
const number = (value) => Number(value || 0).toLocaleString();

export default function UsageStats() {
  const { t } = useTranslation();
  const [period, setPeriod] = useState('24h');
  const [userInput, setUserInput] = useState();
  const [userID, setUserID] = useState(0);
  const [metric, setMetric] = useState('requests');
  const [refresh, setRefresh] = useState(0);
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const currency = getCurrencyConfig();
  const amount = (quota) => {
    const value = quotaToDisplayAmount(quota);
    return currency.type === 'TOKENS'
      ? number(value)
      : `${currency.symbol}${value.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 4 })}`;
  };

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError('');
    setData(null);
    API.get('/api/enhancements/dashboard/usage-stats', {
      params: { period, user_id: userID },
      signal: controller.signal,
      disableDuplicate: true,
      skipErrorHandler: true,
    })
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
  }, [period, userID, refresh, t]);

  const metrics = [
    { value: 'requests', label: t('调用次数') },
    { value: 'token_used', label: t('总 Token') },
    { value: 'quota', label: t('消耗额度') },
  ];
  const chart = useMemo(
    () => ({
      type: 'area',
      height: 280,
      animation: false,
      data: [
        {
          id: 'usage',
          values: (data?.trend || []).map((row) => ({
            hour: dayjs.unix(row.created_at).format('MM-DD HH:mm'),
            value:
              metric === 'quota'
                ? quotaToDisplayAmount(row.quota)
                : Number(row[metric] || 0),
          })),
        },
      ],
      xField: 'hour',
      yField: 'value',
      color: ['#6366f1'],
      point: { visible: false },
      area: { style: { fillOpacity: 0.12 } },
      axes: [
        { orient: 'bottom', label: { autoHide: true, autoRotate: false } },
        { orient: 'left', min: 0 },
      ],
      tooltip: {
        mark: {
          title: { value: (row) => row.hour },
          content: [
            {
              key: () =>
                metric === 'quota'
                  ? t('消耗额度')
                  : metric === 'token_used'
                    ? t('总 Token')
                    : t('调用次数'),
              value: (row) => number(row.value),
            },
          ],
        },
      },
    }),
    [data, metric, t],
  );

  const columns = [
    {
      title: t('调用次数'),
      dataIndex: 'requests',
      render: number,
      align: 'right',
    },
    {
      title: t('总 Token'),
      dataIndex: 'token_used',
      render: number,
      align: 'right',
    },
    {
      title: t('消耗额度'),
      dataIndex: 'quota',
      render: amount,
      align: 'right',
    },
  ];
  const selectUser = (id) => {
    setUserInput(id || undefined);
    setUserID(id);
  };
  const validUser =
    userInput === null ||
    userInput === undefined ||
    userInput === '' ||
    (Number.isSafeInteger(userInput) && userInput >= 0);

  return (
    <div className='flex flex-col gap-4'>
      <Card className='!rounded-lg'>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <Space wrap>
            <span id='usage-period-label' className='sr-only'>
              {t('统计周期')}
            </span>
            <Select
              aria-labelledby='usage-period-label'
              value={period}
              onChange={setPeriod}
              style={{ width: 140 }}
              optionList={[
                { value: '24h', label: t('最近 24 小时') },
                { value: '7d', label: t('最近 7 天') },
                { value: '30d', label: t('最近 30 天') },
              ]}
            />
            <InputNumber
              aria-label={t('筛选用户 ID')}
              placeholder={t('筛选用户 ID')}
              min={0}
              precision={0}
              value={userInput}
              onChange={setUserInput}
              style={{ width: 160 }}
            />
            <Button
              disabled={!validUser || loading}
              onClick={() => selectUser(Number(userInput) || 0)}
            >
              {t('查询')}
            </Button>
            {userID > 0 && (
              <Button onClick={() => selectUser(0)}>{t('全部用户')}</Button>
            )}
            <Tag>
              {userID > 0 ? `${t('用户 ID')} ${userID}` : t('全部用户')}
            </Tag>
          </Space>
          <Button
            icon={<RefreshCw size={16} />}
            loading={loading}
            onClick={() => setRefresh((n) => n + 1)}
          >
            {t('刷新')}
          </Button>
        </div>
        <div className='mt-3 text-xs text-semi-color-text-2'>
          {t(
            '按小时汇总已记录的用量，包含当前未完成的小时；Token 为输入与输出合计。',
          )}
        </div>
        {data && (
          <div className='mt-1 text-xs text-semi-color-text-2'>
            {dayjs.unix(data.start).format('YYYY-MM-DD HH:mm')} –{' '}
            {dayjs.unix(data.generated_at).format('YYYY-MM-DD HH:mm')}
            {' · '}
            {t('数据更新于')} {dayjs.unix(data.generated_at).format('HH:mm:ss')}
            {' · '}
            {t('用量约每 {{minutes}} 分钟入库，页面结果最多缓存 1 分钟。', {
              minutes: data.aggregation_interval_minutes,
            })}
          </div>
        )}
      </Card>

      {error && <Banner type='danger' description={error} closeIcon={null} />}
      {loading && (
        <div className='flex justify-center py-20'>
          <Spin size='large' />
        </div>
      )}
      {!loading && data && (
        <>
          {!data.aggregation_enabled && (
            <Banner
              type='warning'
              closeIcon={null}
              description={t('用量汇总已关闭，当前仅展示此前保存的数据。')}
            />
          )}
          <div className='grid grid-cols-2 xl:grid-cols-4 gap-3'>
            {[
              [t('调用次数'), number(data.totals.requests)],
              [t('总 Token'), number(data.totals.token_used)],
              [t('消耗额度'), amount(data.totals.quota)],
              [t('活跃用户'), number(data.totals.active_users)],
            ].map(([label, value]) => (
              <Card key={label} className='!rounded-lg'>
                <Text type='secondary'>{label}</Text>
                <div className='mt-2 text-xl font-semibold break-all'>
                  {value}
                </div>
              </Card>
            ))}
          </div>
          <Card
            className='!rounded-lg'
            title={t('每小时用量趋势')}
            headerExtraContent={
              <>
                <span id='usage-metric-label' className='sr-only'>
                  {t('趋势指标')}
                </span>
                <Select
                  aria-labelledby='usage-metric-label'
                  value={metric}
                  onChange={setMetric}
                  optionList={metrics}
                  style={{ width: 140 }}
                />
              </>
            }
          >
            {data.totals.requests > 0 ? (
              <VChart spec={chart} option={{ autoFit: true }} />
            ) : (
              <Empty title={t('当前范围暂无用量数据')} />
            )}
          </Card>
          <div className='grid grid-cols-1 xl:grid-cols-2 gap-4'>
            <Card className='!rounded-lg' title={t('模型消耗排行（前 20）')}>
              <Table
                rowKey='model_name'
                size='small'
                pagination={false}
                scroll={{ x: 560 }}
                dataSource={data.models}
                empty={<Empty title={t('当前范围暂无用量数据')} />}
                columns={[
                  { title: t('模型'), dataIndex: 'model_name', width: 180 },
                  ...columns,
                ]}
              />
            </Card>
            <Card className='!rounded-lg' title={t('用户消耗排行（前 20）')}>
              <div className='mb-3 text-xs text-semi-color-text-2'>
                {t('点击用户查看该用户的模型分布与用量趋势。')}
              </div>
              <Table
                rowKey='user_id'
                size='small'
                pagination={false}
                scroll={{ x: 560 }}
                dataSource={data.users}
                empty={<Empty title={t('当前范围暂无用量数据')} />}
                columns={[
                  {
                    title: t('用户'),
                    dataIndex: 'username',
                    width: 180,
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
            </Card>
          </div>
        </>
      )}
    </div>
  );
}
