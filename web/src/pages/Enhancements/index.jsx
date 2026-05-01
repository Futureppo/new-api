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
import { useNavigate, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Card,
  Empty,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Spin,
  Table,
  Tabs,
  TabPane,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import {
  BarChart3,
  Bot,
  Database,
  Gift,
  LineChart,
  RefreshCw,
  ShieldCheck,
  Sparkles,
  UserCog,
} from 'lucide-react';
import { VChart } from '@visactor/react-vchart';
import dayjs from 'dayjs';
import {
  API,
  copy,
  getCurrencyConfig,
  showError,
  showSuccess,
} from '../../helpers';
import {
  displayAmountToQuota,
  getQuotaPerUnit,
  quotaToDisplayAmount,
} from '../../helpers/quota';

const { Title, Text } = Typography;

const SECTIONS = [
  { id: 'dashboard', label: '增强仪表盘', icon: Sparkles },
  { id: 'redemptions', label: '兑换码增强', icon: Gift },
  { id: 'users', label: '用户增强', icon: UserCog },
  { id: 'tokens', label: '令牌审计', icon: ShieldCheck },
  { id: 'risk', label: '风控中心', icon: ShieldCheck },
  { id: 'analytics', label: '日志分析', icon: BarChart3 },
  { id: 'model-status', label: '模型状态', icon: LineChart },
  { id: 'auto-group', label: '自动分组', icon: UserCog },
  { id: 'ai-ban', label: 'AI 封禁', icon: Bot },
  { id: 'system', label: '系统工具', icon: Database },
];

const ENHANCEMENTS_BASE_PATH = '/console/enhancements';
const sectionIds = new Set(SECTIONS.map((section) => section.id));

const FIELD_LABELS = {
  id: 'ID',
  user_id: '用户 ID',
  username: '用户名',
  display_name: '显示名称',
  role: '角色',
  status: '状态',
  disable_reason: '禁用原因',
  email: '邮箱',
  group: '分组',
  key: '密钥',
  name: '名称',
  total: '总数',
  total_count: '总数',
  enabled: '启用',
  disabled: '禁用',
  used: '已使用',
  quota: '额度',
  used_quota: '已用额度',
  remain_quota: '剩余额度',
  unlimited_quota: '无限额度',
  prompt_tokens: '输入 Token',
  completion_tokens: '补全 Token',
  requests: '请求数',
  request_count: '请求次数',
  today_request_count: '今日请求次数',
  today_used_tokens: '今日已用 Token',
  avg_use_time: '平均耗时',
  error_count: '错误数',
  error_rate: '错误率',
  distinct_ips: '不同 IP 数',
  risk_score: '风险评分',
  last_activity: '最后活动',
  created_time: '创建时间',
  redeemed_time: '兑换时间',
  accessed_time: '访问时间',
  expired_time: '过期时间',
  used_user_id: '使用用户 ID',
  used_username: '兑换用户名',
  inviter_id: '邀请人 ID',
  aff_count: '邀请数',
  linux_do_id: 'LinuxDO ID',
  model_name: '模型',
  models: '模型',
  channels: '渠道数',
  tokens: '令牌数',
  redemptions: '兑换码数',
  users: '用户',
  last_24h: '最近 24 小时',
  generated_at: '生成时间',
  time: '时间',
  timestamp: '时间戳',
  time_window_minutes: '时间窗口（分钟）',
  refresh_interval: '刷新间隔',
  sort_mode: '排序方式',
  selected_models: '展示模型',
  site_title: '站点标题',
  theme: '主题',
  public_embed_enabled: '公开嵌入',
  public: '公开',
  window: '时间窗口',
  start: '开始时间',
  end: '结束时间',
  total_users: '用户总数',
  active_users: '活跃用户',
  disabled_users: '禁用用户',
  token_id: '令牌 ID',
  token_name: '令牌名称',
  model_limits_enabled: '模型限制',
  dry_run: '试运行',
  dry_run_default: '默认试运行',
  model: '模型',
  base_url: '接口地址',
  api_key_set: 'API Key 已配置',
  safe_defaults: '安全默认值',
  default_use_auto_group: '默认自动分组',
  auto_groups: '自动分组',
  database: '数据库',
  cache: '缓存',
  runtime: '运行时',
  using_mysql: 'MySQL',
  using_pg: 'PostgreSQL',
  using_sqlite: 'SQLite',
  log_db_split: '日志库独立',
  redis_enabled: 'Redis',
  memory_cache_enabled: '内存缓存',
};

const VALUE_LABELS = {
  true: '是',
  false: '否',
  healthy: '健康',
  degraded: '降级',
  outage: '故障',
  unknown: '未知',
  ok: '正常',
  error: '错误',
  ready: '就绪',
  light: '浅色',
  dark: '深色',
  system: '跟随系统',
  name: '名称',
  status: '状态',
  requests: '请求数',
  error_rate: '错误率',
  custom: '自定义',
  on_demand: '按需计算',
  managed_by_gorm_migrations: '由数据库迁移维护',
  local_logs_only: '仅本地日志',
};

const REDEMPTION_STATUS = {
  UNUSED: 1,
  DISABLED: 2,
  USED: 3,
};

const REDEMPTION_STATUS_META = {
  [REDEMPTION_STATUS.UNUSED]: { color: 'green', text: '未兑换' },
  [REDEMPTION_STATUS.DISABLED]: { color: 'red', text: '已禁用' },
  [REDEMPTION_STATUS.USED]: { color: 'grey', text: '已兑换' },
};

const USER_PREVIEW_KEYS = [
  'id',
  'username',
  'display_name',
  'status',
  'email',
  'quota',
  'used_quota',
  'today_request_count',
  'today_used_tokens',
  'request_count',
  'group',
  'inviter_id',
  'aff_count',
  'linux_do_id',
];

function unwrap(res) {
  if (!res?.data?.success) {
    throw new Error(res?.data?.message || '请求失败');
  }
  return res.data.data;
}

function formatFieldLabel(key, t) {
  if (FIELD_LABELS[key]) return t(FIELD_LABELS[key]);
  if (key.includes('.')) {
    return key
      .split('.')
      .map((part) => formatFieldLabel(part, t))
      .join(' / ');
  }
  return key;
}

function formatNumber(value) {
  if (typeof value !== 'number') return value;
  return new Intl.NumberFormat().format(value);
}

function isUnixTimestampKey(key, value) {
  if (typeof value !== 'number' || value < 1000000000) return false;
  return /(^|_)(time|at)$/.test(key) || key.includes('_time');
}

function formatValue(value, key = '', t = (text) => text) {
  if (value === null || value === undefined || value === '') return '-';
  if (typeof value === 'boolean') return t(value ? '是' : '否');
  if (typeof value === 'string' && VALUE_LABELS[value]) {
    return t(VALUE_LABELS[value]);
  }
  if (isUnixTimestampKey(key, value)) {
    return dayjs.unix(value).format('YYYY-MM-DD HH:mm:ss');
  }
  if (typeof value === 'number') return formatNumber(value);
  if (Array.isArray(value)) {
    return value.length
      ? value.map((item) => formatValue(item, key, t)).join(', ')
      : '-';
  }
  if (typeof value === 'object') {
    return Object.entries(value)
      .map(
        ([childKey, childValue]) =>
          `${formatFieldLabel(childKey, t)}：${formatValue(childValue, childKey, t)}`,
      )
      .join('；');
  }
  return String(value);
}

function pickItems(data) {
  if (Array.isArray(data)) return data;
  if (Array.isArray(data?.items)) return data.items;
  if (Array.isArray(data?.candidates)) return data.candidates;
  return [];
}

function SummaryGrid({ data }) {
  const { t } = useTranslation();
  const entries = Object.entries(data || {}).flatMap(([key, value]) => {
    if (
      value &&
      typeof value === 'object' &&
      !Array.isArray(value) &&
      Object.keys(value).length > 0
    ) {
      return Object.entries(value)
        .filter(([, childValue]) => typeof childValue !== 'object')
        .map(([childKey, childValue]) => [`${key}.${childKey}`, childValue]);
    }
    return [[key, value]];
  });
  if (entries.length === 0) return null;

  return (
    <div className='grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-3'>
      {entries.map(([key, value]) => (
        <Card key={key} bodyStyle={{ padding: 16 }} className='!rounded-lg'>
          <Text type='secondary' size='small'>
            {formatFieldLabel(key, t)}
          </Text>
          <div className='text-2xl font-semibold mt-2 text-semi-color-text-0 break-words'>
            {formatValue(value, key, t)}
          </div>
        </Card>
      ))}
    </div>
  );
}

function DataPreview({
  data,
  limit = 12,
  keys: preferredKeys,
  valueFormatter,
  pagination = false,
  loading = false,
}) {
  const { t } = useTranslation();
  const rawRows = pickItems(data);
  const rows = typeof limit === 'number' ? rawRows.slice(0, limit) : rawRows;
  if (rows.length === 0) {
    return <Empty image={<></>} title={t('暂无数据')} />;
  }

  const keys =
    preferredKeys ||
    Array.from(
      rows.reduce((set, row) => {
        Object.keys(row || {}).forEach((key) => set.add(key));
        return set;
      }, new Set()),
    ).slice(0, 8);
  const renderValue = valueFormatter || formatValue;

  const columns = keys.map((key) => ({
    title: formatFieldLabel(key, t),
    dataIndex: key,
    key,
    render: (value) => (
      <span className='break-words text-sm'>{renderValue(value, key, t)}</span>
    ),
  }));

  return (
    <Table
      size='small'
      columns={columns}
      dataSource={rows.map((row, index) => ({ ...row, _rowKey: index }))}
      rowKey='_rowKey'
      pagination={pagination}
      loading={loading}
      scroll={{ x: 'max-content' }}
    />
  );
}

function DashboardPanel({ data }) {
  const overview = data?.overview || {};
  const trend = data?.trend || [];
  const topUsers = data?.topUsers || [];
  const modelUsage = data?.models || [];

  const spec = useMemo(
    () => ({
      type: 'line',
      data: {
        values: trend.map((item) => ({
          time: item.time,
          requests: item.requests,
          quota: item.quota,
        })),
      },
      xField: 'time',
      yField: 'requests',
      point: { visible: true },
      line: { style: { curveType: 'monotone' } },
      axes: [
        { orient: 'bottom', label: { autoHide: true } },
        { orient: 'left' },
      ],
      tooltip: { visible: true },
    }),
    [trend],
  );

  return (
    <div className='space-y-4'>
      <SummaryGrid data={overview.last_24h || overview} />
      <Card title='调用趋势' className='!rounded-lg'>
        <div className='h-72'>
          {trend.length > 0 ? (
            <VChart spec={spec} />
          ) : (
            <Empty title='暂无趋势数据' />
          )}
        </div>
      </Card>
      <div className='grid grid-cols-1 xl:grid-cols-2 gap-4'>
        <Card title='高用量用户' className='!rounded-lg'>
          <DataPreview data={topUsers} />
        </Card>
        <Card title='模型用量' className='!rounded-lg'>
          <DataPreview data={modelUsage} />
        </Card>
      </div>
    </div>
  );
}

function isRedemptionExpired(record) {
  return (
    record?.status === REDEMPTION_STATUS.UNUSED &&
    record.expired_time !== 0 &&
    record.expired_time < Math.floor(Date.now() / 1000)
  );
}

function renderRedemptionStatus(record, t) {
  if (isRedemptionExpired(record)) {
    return <Tag color='orange'>{t('已过期')}</Tag>;
  }
  const meta = REDEMPTION_STATUS_META[record?.status] || {
    color: 'black',
    text: '未知',
  };
  return <Tag color={meta.color}>{t(meta.text)}</Tag>;
}

function redemptionUserText(record) {
  if (!record?.used_user_id) return '-';
  const username = record.used_username || '-';
  return `${username} (#${record.used_user_id})`;
}

function formatDisplayAmount(quota, currency) {
  const amount = quotaToDisplayAmount(quota);
  const formatted = new Intl.NumberFormat(undefined, {
    maximumFractionDigits: 6,
  }).format(amount);
  if (currency.type === 'TOKENS') return formatted;
  return `${currency.symbol}${formatted}`;
}

function formatQuotaAsAmount(quota, currency) {
  const amount = Number(quota || 0) / getQuotaPerUnit();
  const shouldUseConfiguredCurrency =
    currency.type === 'CNY' || currency.type === 'CUSTOM';
  const convertedAmount = shouldUseConfiguredCurrency
    ? amount * (currency.rate || 1)
    : amount;
  const symbol = shouldUseConfiguredCurrency ? currency.symbol : '$';
  const formatted = new Intl.NumberFormat(undefined, {
    maximumFractionDigits: 6,
  }).format(convertedAmount);
  return `${symbol}${formatted}`;
}

function RedemptionsPanel({ data }) {
  const { t } = useTranslation();
  const [form, setForm] = useState({
    count: 1,
    amount: 1,
    name: '增强管理',
  });
  const [statistics, setStatistics] = useState(data?.statistics || {});
  const [list, setList] = useState(
    data?.list || { items: [], total: 0, page: 1, page_size: 20 },
  );
  const [filters, setFilters] = useState({ status: '0', keyword: '' });
  const [pageSize, setPageSize] = useState(data?.list?.page_size || 20);
  const [listLoading, setListLoading] = useState(false);
  const [generated, setGenerated] = useState([]);
  const [generating, setGenerating] = useState(false);
  const generatedQuota = useMemo(
    () => displayAmountToQuota(form.amount),
    [form.amount],
  );
  const currency = getCurrencyConfig();

  useEffect(() => {
    setStatistics(data?.statistics || {});
  }, [data?.statistics]);

  useEffect(() => {
    if (data?.list) {
      setList(data.list);
      setPageSize(data.list.page_size || 20);
    }
  }, [data?.list]);

  const loadStatistics = async () => {
    const nextStatistics = await API.get(
      '/api/enhancements/redemptions/statistics',
    ).then(unwrap);
    setStatistics(nextStatistics || {});
  };

  const loadRedemptions = async (
    page = 1,
    size = pageSize,
    nextFilters = filters,
  ) => {
    setListLoading(true);
    try {
      const params = new URLSearchParams({
        p: String(page),
        page_size: String(size),
      });
      if (nextFilters.status !== '0') {
        params.set('status', nextFilters.status);
      }
      const keyword = nextFilters.keyword.trim();
      if (keyword) {
        params.set('keyword', keyword);
      }
      const nextList = await API.get(
        `/api/enhancements/redemptions?${params.toString()}`,
      ).then(unwrap);
      setList(nextList || { items: [], total: 0, page, page_size: size });
    } catch (error) {
      showError(error.message || error);
    } finally {
      setListLoading(false);
    }
  };

  const updateRedemptionEnabled = (record, enabled) => {
    Modal.confirm({
      title: enabled ? t('启用兑换码') : t('禁用兑换码'),
      content: enabled
        ? t('确认启用这个兑换码？')
        : t('确认禁用这个未兑换的兑换码？'),
      okText: enabled ? t('启用') : t('禁用'),
      cancelText: t('取消'),
      onOk: async () => {
        try {
          await API.post(
            `/api/enhancements/redemptions/${record.id}/${enabled ? 'enable' : 'disable'}`,
          );
          showSuccess(t('操作成功'));
          await Promise.all([
            loadStatistics(),
            loadRedemptions(list?.page || 1, pageSize),
          ]);
        } catch (error) {
          showError(error.message || error);
        }
      },
    });
  };

  const copyGeneratedKeys = async () => {
    const keys = generated.map((item) => item.key).filter(Boolean);
    if (keys.length === 0) return;
    if (await copy(keys.join('\n'))) {
      showSuccess(t('复制成功'));
    } else {
      showError(t('复制失败'));
    }
  };

  const generate = () => {
    Modal.confirm({
      title: t('生成兑换码'),
      content: t('确认生成兑换码？'),
      okText: t('确认'),
      cancelText: t('取消'),
      onOk: async () => {
        setGenerating(true);
        try {
          const res = await API.post('/api/enhancements/redemptions/generate', {
            ...form,
            quota: generatedQuota,
          });
          const rows = unwrap(res);
          setGenerated(rows || []);
          showSuccess(t('生成成功'));
          await Promise.all([loadStatistics(), loadRedemptions(1, pageSize)]);
        } catch (error) {
          showError(error.message || error);
        } finally {
          setGenerating(false);
        }
      },
    });
  };

  const columns = [
    {
      title: t('ID'),
      dataIndex: 'id',
      width: 80,
    },
    {
      title: t('名称'),
      dataIndex: 'name',
      width: 160,
    },
    {
      title: t('兑换码'),
      dataIndex: 'key',
      width: 180,
      render: (value) => <span className='font-mono text-xs'>{value}</span>,
    },
    {
      title: t('状态'),
      dataIndex: 'status',
      width: 110,
      render: (_, record) => renderRedemptionStatus(record, t),
    },
    {
      title: t('金额'),
      dataIndex: 'quota',
      width: 130,
      render: (value) => (
        <Tag color='blue' shape='circle'>
          {formatDisplayAmount(value, currency)}
        </Tag>
      ),
    },
    {
      title: t('兑换用户'),
      dataIndex: 'used_username',
      width: 180,
      render: (_, record) => redemptionUserText(record),
    },
    {
      title: t('兑换时间'),
      dataIndex: 'redeemed_time',
      width: 180,
      render: (value) => formatValue(value, 'redeemed_time', t),
    },
    {
      title: t('创建时间'),
      dataIndex: 'created_time',
      width: 180,
      render: (value) => formatValue(value, 'created_time', t),
    },
    {
      title: t('过期时间'),
      dataIndex: 'expired_time',
      width: 180,
      render: (value) =>
        value === 0 ? t('永不过期') : formatValue(value, 'expired_time', t),
    },
    {
      title: t('操作'),
      dataIndex: 'operate',
      fixed: 'right',
      width: 110,
      render: (_, record) => {
        if (record.status === REDEMPTION_STATUS.DISABLED) {
          return (
            <Button
              size='small'
              type='primary'
              onClick={() => updateRedemptionEnabled(record, true)}
            >
              {t('启用')}
            </Button>
          );
        }
        return (
          <Button
            size='small'
            type='danger'
            disabled={record.status !== REDEMPTION_STATUS.UNUSED}
            onClick={() => updateRedemptionEnabled(record, false)}
          >
            {t('禁用')}
          </Button>
        );
      },
    },
  ];

  return (
    <div className='space-y-4'>
      <SummaryGrid data={statistics} />
      <Card title='批量生成' className='!rounded-lg'>
        <div className='grid grid-cols-1 md:grid-cols-4 gap-3 items-end'>
          <label className='space-y-1'>
            <Text type='secondary'>名称</Text>
            <Input
              value={form.name}
              onChange={(value) =>
                setForm((prev) => ({ ...prev, name: value }))
              }
            />
          </label>
          <label className='space-y-1'>
            <Text type='secondary'>数量</Text>
            <InputNumber
              min={1}
              max={100}
              value={form.count}
              onChange={(value) =>
                setForm((prev) => ({ ...prev, count: value || 1 }))
              }
            />
          </label>
          <label className='space-y-1'>
            <Text type='secondary'>金额</Text>
            <InputNumber
              min={1}
              prefix={currency.symbol}
              precision={6}
              value={form.amount}
              onChange={(value) =>
                setForm((prev) => ({ ...prev, amount: value || 1 }))
              }
            />
          </label>
          <Button
            type='primary'
            icon={<Gift size={16} />}
            loading={generating}
            onClick={generate}
          >
            {t('生成')}
          </Button>
        </div>
      </Card>
      {generated.length > 0 && (
        <Card title='本次生成结果' className='!rounded-lg'>
          <div className='mb-3'>
            <Button type='primary' onClick={copyGeneratedKeys}>
              {t('一键复制兑换码')}
            </Button>
          </div>
          <DataPreview data={generated} />
        </Card>
      )}
      <Card title='兑换码列表' className='!rounded-lg'>
        <div className='flex flex-col lg:flex-row gap-3 mb-4'>
          <Select
            value={filters.status}
            style={{ width: 160 }}
            onChange={(value) => {
              const nextFilters = { ...filters, status: String(value) };
              setFilters(nextFilters);
              loadRedemptions(1, pageSize, nextFilters);
            }}
          >
            <Select.Option value='0'>{t('全部')}</Select.Option>
            <Select.Option value='1'>{t('未兑换')}</Select.Option>
            <Select.Option value='3'>{t('已兑换')}</Select.Option>
            <Select.Option value='2'>{t('已禁用')}</Select.Option>
          </Select>
          <Input
            value={filters.keyword}
            placeholder={t('搜索兑换用户名或用户 ID')}
            onChange={(value) =>
              setFilters((prev) => ({ ...prev, keyword: value }))
            }
            onEnterPress={() => loadRedemptions(1, pageSize)}
            className='lg:max-w-sm'
          />
          <Space>
            <Button type='primary' onClick={() => loadRedemptions(1, pageSize)}>
              {t('搜索')}
            </Button>
            <Button
              onClick={() => {
                const nextFilters = { status: '0', keyword: '' };
                setFilters(nextFilters);
                loadRedemptions(1, pageSize, nextFilters);
              }}
            >
              {t('重置')}
            </Button>
          </Space>
        </div>
        <Table
          size='small'
          columns={columns}
          dataSource={(list?.items || []).map((row) => ({
            ...row,
            _rowKey: row.id,
          }))}
          rowKey='_rowKey'
          loading={listLoading}
          scroll={{ x: 'max-content' }}
          pagination={{
            currentPage: list?.page || 1,
            pageSize,
            total: list?.total || 0,
            showSizeChanger: true,
            pageSizeOptions: [10, 20, 50, 100],
            onPageChange: (page) => loadRedemptions(page, pageSize),
            onPageSizeChange: (size) => {
              setPageSize(size);
              loadRedemptions(1, size);
            },
          }}
        />
      </Card>
    </div>
  );
}

function UsersPanel({ data }) {
  const currency = getCurrencyConfig();
  const [list, setList] = useState(
    data?.list || { items: [], total: 0, page: 1, page_size: 20 },
  );
  const [pageSize, setPageSize] = useState(data?.list?.page_size || 20);
  const [listLoading, setListLoading] = useState(false);

  useEffect(() => {
    if (data?.list) {
      setList(data.list);
      setPageSize(data.list.page_size || 20);
    }
  }, [data?.list]);

  const loadUsers = async (page = 1, size = pageSize) => {
    setListLoading(true);
    try {
      const params = new URLSearchParams({
        p: String(page),
        page_size: String(size),
      });
      const nextList = await API.get(
        `/api/enhancements/users?${params.toString()}`,
      ).then(unwrap);
      setList(nextList || { items: [], total: 0, page, page_size: size });
    } catch (error) {
      showError(error.message || error);
    } finally {
      setListLoading(false);
    }
  };

  const formatUserValue = (value, key, t) => {
    if (key === 'email' && value === '***masked***') {
      return t('未绑定');
    }
    if (key === 'quota' || key === 'used_quota') {
      return formatQuotaAsAmount(value, currency);
    }
    return formatValue(value, key, t);
  };

  return (
    <div className='space-y-4'>
      <SummaryGrid data={data?.summary || {}} />
      <Card title='数据预览' className='!rounded-lg'>
        <DataPreview
          data={list}
          limit={null}
          keys={USER_PREVIEW_KEYS}
          valueFormatter={formatUserValue}
          loading={listLoading}
          pagination={{
            currentPage: list?.page || 1,
            pageSize,
            total: list?.total || 0,
            showSizeChanger: true,
            pageSizeOptions: [10, 20, 50, 100],
            onPageChange: (page) => loadUsers(page, pageSize),
            onPageSizeChange: (size) => {
              setPageSize(size);
              loadUsers(1, size);
            },
          }}
        />
      </Card>
    </div>
  );
}

function GenericSection({ section, data, onRefresh }) {
  if (section === 'dashboard') {
    return <DashboardPanel data={data} />;
  }
  if (section === 'redemptions') {
    return <RedemptionsPanel data={data} onRefresh={onRefresh} />;
  }
  if (section === 'users') {
    return <UsersPanel data={data} />;
  }

  const summary =
    data?.summary || data?.statistics || data?.config || data?.overview || data;
  const list =
    data?.list ||
    data?.ranking ||
    data?.models ||
    data?.statuses ||
    data?.preview ||
    data;

  return (
    <div className='space-y-4'>
      <SummaryGrid data={summary || {}} />
      <Card title='数据预览' className='!rounded-lg'>
        <DataPreview data={list} />
      </Card>
    </div>
  );
}

async function fetchSection(section) {
  switch (section) {
    case 'dashboard': {
      const [overview, trend, topUsers, models] = await Promise.all([
        API.get('/api/enhancements/dashboard/overview').then(unwrap),
        API.get('/api/enhancements/dashboard/trends/hourly').then(unwrap),
        API.get('/api/enhancements/dashboard/top-users').then(unwrap),
        API.get('/api/enhancements/dashboard/models').then(unwrap),
      ]);
      return { overview, trend, topUsers, models };
    }
    case 'redemptions': {
      const [statistics, list] = await Promise.all([
        API.get('/api/enhancements/redemptions/statistics').then(unwrap),
        API.get('/api/enhancements/redemptions').then(unwrap),
      ]);
      return { statistics, list };
    }
    case 'users': {
      const [summary, list] = await Promise.all([
        API.get('/api/enhancements/users/activity-stats').then(unwrap),
        API.get('/api/enhancements/users').then(unwrap),
      ]);
      return { summary, list };
    }
    case 'tokens': {
      const [statistics, list] = await Promise.all([
        API.get('/api/enhancements/tokens/statistics').then(unwrap),
        API.get('/api/enhancements/tokens').then(unwrap),
      ]);
      return { statistics, list };
    }
    case 'risk': {
      const ranking = await API.get('/api/enhancements/risk/leaderboards').then(
        unwrap,
      );
      return { ranking };
    }
    case 'analytics': {
      const [summary, models] = await Promise.all([
        API.get('/api/enhancements/analytics/summary').then(unwrap),
        API.get('/api/enhancements/analytics/models').then(unwrap),
      ]);
      return { summary, models };
    }
    case 'model-status': {
      const [config, statuses] = await Promise.all([
        API.get('/api/enhancements/model-status/config/time-window').then(
          unwrap,
        ),
        API.get('/api/enhancements/model-status/status/all').then(unwrap),
      ]);
      return { config, statuses };
    }
    case 'auto-group': {
      const [config, preview] = await Promise.all([
        API.get('/api/enhancements/auto-group/config').then(unwrap),
        API.get('/api/enhancements/auto-group/preview').then(unwrap),
      ]);
      return { config, preview };
    }
    case 'ai-ban': {
      const [config, ranking] = await Promise.all([
        API.get('/api/enhancements/ai-ban/config').then(unwrap),
        API.get('/api/enhancements/ai-ban/suspicious').then(unwrap),
      ]);
      return { config, ranking };
    }
    case 'system': {
      const summary = await API.get('/api/enhancements/system/info').then(
        unwrap,
      );
      return { summary };
    }
    default:
      return {};
  }
}

export default function Enhancements() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const params = useParams();
  const activeSection = params.section || 'dashboard';
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!sectionIds.has(activeSection)) {
      navigate(`${ENHANCEMENTS_BASE_PATH}/dashboard`, { replace: true });
    }
  }, [activeSection, navigate]);

  const activeMeta =
    SECTIONS.find((section) => section.id === activeSection) || SECTIONS[0];

  const loadData = async () => {
    if (!sectionIds.has(activeSection)) return;
    setLoading(true);
    setError('');
    try {
      setData(await fetchSection(activeSection));
    } catch (err) {
      const message = err?.message || '加载失败';
      setError(message);
      showError(message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
  }, [activeSection]);

  const Icon = activeMeta.icon;

  return (
    <div className='mt-[60px] px-2 pb-6'>
      <div className='flex flex-col lg:flex-row lg:items-center lg:justify-between gap-3 mb-4'>
        <div className='flex items-center gap-3'>
          <div className='w-10 h-10 rounded-lg flex items-center justify-center bg-semi-color-fill-0 border border-semi-color-border'>
            <Icon size={20} />
          </div>
          <div>
            <Title heading={4} style={{ margin: 0 }}>
              {t('增强管理')}
            </Title>
            <Text type='secondary'>{t(activeMeta.label)}</Text>
          </div>
        </div>
        <Space>
          {activeSection === 'ai-ban' && <Tag color='blue'>{t('试运行')}</Tag>}
          <Button
            icon={<RefreshCw size={16} />}
            onClick={loadData}
            loading={loading}
          >
            {t('刷新')}
          </Button>
        </Space>
      </div>

      <Card className='!rounded-lg mb-4' bodyStyle={{ paddingBottom: 0 }}>
        <Tabs
          type='line'
          activeKey={activeSection}
          onChange={(key) => navigate(`${ENHANCEMENTS_BASE_PATH}/${key}`)}
        >
          {SECTIONS.map((section) => (
            <TabPane
              tab={t(section.label)}
              itemKey={section.id}
              key={section.id}
            />
          ))}
        </Tabs>
      </Card>

      {loading ? (
        <div className='py-20 flex justify-center'>
          <Spin size='large' />
        </div>
      ) : error ? (
        <Card className='!rounded-lg'>
          <Empty title={error} />
        </Card>
      ) : (
        <GenericSection
          section={activeSection}
          data={data}
          onRefresh={loadData}
        />
      )}
    </div>
  );
}
