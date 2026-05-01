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

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Avatar,
  Card,
  DatePicker,
  Empty,
  Input,
  InputNumber,
  Modal,
  Select,
  SideSheet,
  Space,
  Spin,
  Switch,
  Table,
  Tabs,
  TabPane,
  Tag,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import {
  Activity,
  AlertTriangle,
  Bot,
  CheckCircle2,
  Copy as CopyIcon,
  CreditCard,
  Database,
  ExternalLink,
  Gift,
  Globe2,
  KeyRound,
  LineChart,
  Link2,
  RefreshCw,
  Save,
  ShieldCheck,
  Sparkles,
  UserCog,
  X,
} from 'lucide-react';
import { VChart } from '@visactor/react-vchart';
import dayjs from 'dayjs';
import {
  API,
  copy,
  getCurrencyConfig,
  getModelCategories,
  getServerAddress,
  renderGroupOption,
  selectFilter,
  showError,
  showSuccess,
} from '../../helpers';
import {
  displayAmountToQuota,
  quotaToDisplayAmount,
} from '../../helpers/quota';

const { Title, Text } = Typography;

const SECTIONS = [
  { id: 'dashboard', label: '增强仪表盘', icon: Sparkles },
  { id: 'redemptions', label: '兑换码增强', icon: Gift },
  { id: 'users', label: '用户增强', icon: UserCog },
  { id: 'tokens', label: '令牌审计', icon: ShieldCheck },
  { id: 'risk', label: '风控中心', icon: ShieldCheck },
  { id: 'model-status', label: '模型状态', icon: LineChart },
  { id: 'auto-group', label: '自动分组', icon: UserCog },
  { id: 'ai-ban', label: 'AI 封禁', icon: Bot },
  { id: 'system', label: '系统工具', icon: Database },
];

const ENHANCEMENTS_BASE_PATH = '/console/enhancements';
const sectionIds = new Set(SECTIONS.map((section) => section.id));
const MODEL_STATUS_PUBLIC_PATH = '/model-status';
const MODEL_STATUS_WINDOWS = [
  { label: '今日', value: 'today' },
  { label: '24h', value: '24h' },
  { label: '7天', value: '7d' },
  { label: '30天', value: '30d' },
];
const MODEL_STATUS_SORT_OPTIONS = [
  { label: '请求次数降序', value: 'requests_desc' },
  { label: '成功率升序', value: 'success_rate_asc' },
];

const MODEL_STATUS_META = {
  green: {
    label: '正常',
    color: 'green',
    icon: CheckCircle2,
    barClass: 'bg-emerald-500',
    softClass: 'bg-emerald-50 text-emerald-700 border-emerald-100',
  },
  yellow: {
    label: '警告',
    color: 'amber',
    icon: AlertTriangle,
    barClass: 'bg-amber-400',
    softClass: 'bg-amber-50 text-amber-700 border-amber-100',
  },
  red: {
    label: '异常',
    color: 'red',
    icon: AlertTriangle,
    barClass: 'bg-rose-500',
    softClass: 'bg-rose-50 text-rose-700 border-rose-100',
  },
};

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
  quota: '金额',
  used_quota: '已用金额',
  remain_quota: '剩余金额',
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
  model_limits: '限制模型',
  allow_ips: '允许 IP',
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

const TOKEN_STATUS = {
  ENABLED: 1,
  DISABLED: 2,
  EXPIRED: 3,
  EXHAUSTED: 4,
};

const TOKEN_STATUS_META = {
  [TOKEN_STATUS.ENABLED]: { color: 'green', text: '启用' },
  [TOKEN_STATUS.DISABLED]: { color: 'red', text: '禁用' },
  [TOKEN_STATUS.EXPIRED]: { color: 'orange', text: '已过期' },
  [TOKEN_STATUS.EXHAUSTED]: { color: 'grey', text: '已耗尽' },
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

function formatPercent(value) {
  const number = Number(value || 0);
  return `${(number * 100).toFixed(1)}%`;
}

function formatStatusPercent(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return '100.0%';
  return `${number.toFixed(1)}%`;
}

function getModelStatusMeta(status) {
  return MODEL_STATUS_META[status] || MODEL_STATUS_META.green;
}

function getModelStatusPublicUrl(config = {}) {
  const base = String(config.server_address || getServerAddress() || '')
    .trim()
    .replace(/\/+$/, '');
  const origin = base || window.location.origin;
  return `${origin}${config.public_url_path || MODEL_STATUS_PUBLIC_PATH}`;
}

function getModelStatusConfigWindow(config = {}) {
  return config.current_window || config.default_window || '24h';
}

function getModelStatusRefreshMinutes(config = {}) {
  const explicit = Number(config.refresh_interval_minutes);
  if (Number.isFinite(explicit) && explicit > 0) {
    return Math.min(1440, Math.max(1, Math.round(explicit)));
  }
  const seconds = Number(config.refresh_interval || 60);
  if (!Number.isFinite(seconds) || seconds <= 0) return 1;
  return Math.min(1440, Math.max(1, Math.round(seconds / 60)));
}

function getModelStatusSlotMinutes(config = {}) {
  const minutes = Number(config.slot_minutes || 30);
  if (!Number.isFinite(minutes)) return 30;
  return Math.min(1440, Math.max(5, Math.round(minutes)));
}

function getModelStatusThreshold(config = {}, key, fallback) {
  const value = Number(config[key]);
  if (!Number.isFinite(value)) return fallback;
  return Math.min(100, Math.max(1, value));
}

function modelStatusWindowToMinutes(windowValue) {
  switch (windowValue) {
    case 'today':
      return 0;
    case '7d':
      return 7 * 24 * 60;
    case '30d':
      return 30 * 24 * 60;
    case '24h':
    default:
      return 24 * 60;
  }
}

function modelStatusOverview(statuses = []) {
  const totalModels = statuses.length;
  const totalRequests = statuses.reduce(
    (sum, item) => sum + Number(item.total_requests || 0),
    0,
  );
  const successCount = statuses.reduce(
    (sum, item) => sum + Number(item.success_count || 0),
    0,
  );
  const statusCounts = statuses.reduce(
    (counts, item) => {
      const key = item.current_status || 'green';
      counts[key] = (counts[key] || 0) + 1;
      return counts;
    },
    { green: 0, yellow: 0, red: 0 },
  );
  const successRate =
    totalRequests > 0 ? (successCount / totalRequests) * 100 : 100;
  return {
    totalModels,
    totalRequests,
    successRate,
    statusCounts,
  };
}

function isUnixTimestampKey(key, value) {
  if (typeof value !== 'number' || value < 1000000000) return false;
  return /(^|_)(time|at)$/.test(key) || key.includes('_time');
}

function isQuotaAmountKey(key = '') {
  const field = String(key).split('.').pop();
  return (
    field === 'quota' ||
    (field.endsWith('_quota') && field !== 'unlimited_quota')
  );
}

function formatValue(value, key = '', t = (text) => text) {
  if (value === null || value === undefined || value === '') return '-';
  if (typeof value === 'boolean') return t(value ? '是' : '否');
  if (typeof value === 'string' && VALUE_LABELS[value]) {
    return t(VALUE_LABELS[value]);
  }
  if (isQuotaAmountKey(key) && typeof value === 'number') {
    return formatDisplayAmount(value);
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

function renderTokenStatus(status, t) {
  const meta = TOKEN_STATUS_META[status] || {
    color: 'black',
    text: '未知',
  };
  return <Tag color={meta.color}>{t(meta.text)}</Tag>;
}

function formatDisplayAmount(quota, currency = getCurrencyConfig()) {
  const amount = quotaToDisplayAmount(quota);
  const formatted = new Intl.NumberFormat(undefined, {
    maximumFractionDigits: 6,
  }).format(amount);
  if (currency.type === 'TOKENS') return formatted;
  return `${currency.symbol}${formatted}`;
}

function formatQuotaAsAmount(quota, currency) {
  return formatDisplayAmount(quota, currency);
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

function TokensPanel({ data }) {
  const { t } = useTranslation();
  const currency = getCurrencyConfig();
  const [statistics, setStatistics] = useState(data?.statistics || {});
  const [list, setList] = useState(
    data?.list || { items: [], total: 0, page: 1, page_size: 20 },
  );
  const [filters, setFilters] = useState({ status: '0', key: '', group: '' });
  const [pageSize, setPageSize] = useState(data?.list?.page_size || 20);
  const [listLoading, setListLoading] = useState(false);
  const [editingToken, setEditingToken] = useState(null);
  const [editForm, setEditForm] = useState(null);
  const [groups, setGroups] = useState([]);
  const [models, setModels] = useState([]);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setStatistics(data?.statistics || {});
  }, [data?.statistics]);

  useEffect(() => {
    if (data?.list) {
      setList(data.list);
      setPageSize(data.list.page_size || 20);
    }
  }, [data?.list]);

  useEffect(() => {
    const loadOptions = async () => {
      try {
        const [groupsRes, modelsRes] = await Promise.all([
          API.get('/api/user/self/groups'),
          API.get('/api/user/models'),
        ]);
        if (groupsRes.data?.success) {
          const groupOptions = Object.entries(groupsRes.data.data || {}).map(
            ([group, info]) => ({
              label: info.desc,
              value: group,
              ratio: info.ratio,
            }),
          );
          groupOptions.sort((a, b) => {
            if (a.value === 'auto') return -1;
            if (b.value === 'auto') return 1;
            return a.value.localeCompare(b.value);
          });
          setGroups(groupOptions);
        } else if (groupsRes.data?.message) {
          showError(t(groupsRes.data.message));
        }
        if (modelsRes.data?.success) {
          const categories = getModelCategories(t);
          const modelOptions = (modelsRes.data.data || []).map((model) => {
            let icon = null;
            for (const [key, category] of Object.entries(categories)) {
              if (key !== 'all' && category.filter({ model_name: model })) {
                icon = category.icon;
                break;
              }
            }
            return {
              label: (
                <span className='flex items-center gap-1'>
                  {icon}
                  {model}
                </span>
              ),
              value: model,
            };
          });
          setModels(modelOptions);
        } else if (modelsRes.data?.message) {
          showError(t(modelsRes.data.message));
        }
      } catch (error) {
        showError(error.message || error);
      }
    };
    loadOptions();
  }, [t]);

  const loadStatistics = async () => {
    const nextStatistics = await API.get(
      '/api/enhancements/tokens/statistics',
    ).then(unwrap);
    setStatistics(nextStatistics || {});
  };

  const loadTokens = async (
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
      if (nextFilters.status !== '0') params.set('status', nextFilters.status);
      if (nextFilters.key.trim()) params.set('key', nextFilters.key.trim());
      if (nextFilters.group.trim()) {
        params.set('group', nextFilters.group.trim());
      }
      const nextList = await API.get(
        `/api/enhancements/tokens?${params.toString()}`,
      ).then(unwrap);
      setList(nextList || { items: [], total: 0, page, page_size: size });
    } catch (error) {
      showError(error.message || error);
    } finally {
      setListLoading(false);
    }
  };

  const openEditToken = (record) => {
    const modelLimits =
      typeof record.model_limits === 'string' && record.model_limits.trim()
        ? record.model_limits
            .split(',')
            .map((model) => model.trim())
            .filter(Boolean)
        : [];
    setEditingToken(record);
    setEditForm({
      name: record.name || '',
      status: record.status || TOKEN_STATUS.ENABLED,
      group: record.group || '',
      expired_time: record.expired_time ?? -1,
      remain_quota: record.remain_quota || 0,
      remain_amount: Number(
        quotaToDisplayAmount(record.remain_quota || 0).toFixed(6),
      ),
      unlimited_quota: Boolean(record.unlimited_quota),
      model_limits_enabled: Boolean(record.model_limits_enabled),
      model_limits: modelLimits,
      allow_ips: record.allow_ips || '',
    });
  };

  const patchEditForm = (patch) => {
    setEditForm((prev) => ({ ...(prev || {}), ...patch }));
  };

  const saveToken = async () => {
    if (!editingToken || !editForm) return;
    setSaving(true);
    try {
      const modelLimits = Array.isArray(editForm.model_limits)
        ? editForm.model_limits.join(',')
        : editForm.model_limits.trim();
      await API.put(`/api/enhancements/tokens/${editingToken.id}`, {
        ...editForm,
        name: editForm.name.trim(),
        group: editForm.group.trim(),
        model_limits: modelLimits,
        model_limits_enabled: modelLimits !== '',
        allow_ips: editForm.allow_ips.trim(),
        status: Number(editForm.status),
        expired_time: Number(editForm.expired_time),
        remain_quota: Number(editForm.remain_quota),
      });
      showSuccess(t('保存成功'));
      setEditingToken(null);
      setEditForm(null);
      await Promise.all([
        loadStatistics(),
        loadTokens(list?.page || 1, pageSize),
      ]);
    } catch (error) {
      showError(error.message || error);
    } finally {
      setSaving(false);
    }
  };

  const setTokenExpiration = (months, days, hours) => {
    if (months === 0 && days === 0 && hours === 0) {
      patchEditForm({ expired_time: -1 });
      return;
    }
    const date = new Date();
    date.setMonth(date.getMonth() + months);
    date.setDate(date.getDate() + days);
    date.setHours(date.getHours() + hours);
    patchEditForm({ expired_time: Math.ceil(date.getTime() / 1000) });
  };

  const groupOptions = useMemo(() => {
    if (
      !editForm?.group ||
      groups.some((group) => group.value === editForm.group)
    ) {
      return groups;
    }
    return [
      ...groups,
      {
        label: editForm.group,
        value: editForm.group,
      },
    ];
  }, [editForm?.group, groups]);

  const modelOptions = useMemo(() => {
    const selectedModels = Array.isArray(editForm?.model_limits)
      ? editForm.model_limits
      : [];
    const extraOptions = selectedModels
      .filter(
        (model) => model && !models.some((option) => option.value === model),
      )
      .map((model) => ({ label: model, value: model }));
    return [...models, ...extraOptions];
  }, [editForm?.model_limits, models]);

  const columns = [
    { title: t('ID'), dataIndex: 'id', width: 80 },
    { title: t('用户 ID'), dataIndex: 'user_id', width: 100 },
    { title: t('名称'), dataIndex: 'name', width: 160 },
    {
      title: t('Key'),
      dataIndex: 'key',
      width: 190,
      render: (value) => <span className='font-mono text-xs'>{value}</span>,
    },
    {
      title: t('状态'),
      dataIndex: 'status',
      width: 110,
      render: (value) => renderTokenStatus(value, t),
    },
    {
      title: t('分组'),
      dataIndex: 'group',
      width: 120,
      render: (value) => value || '-',
    },
    {
      title: t('剩余金额'),
      dataIndex: 'remain_quota',
      width: 130,
      render: (value) => formatDisplayAmount(value, currency),
    },
    {
      title: t('已用金额'),
      dataIndex: 'used_quota',
      width: 130,
      render: (value) => formatDisplayAmount(value, currency),
    },
    {
      title: t('无限额度'),
      dataIndex: 'unlimited_quota',
      width: 110,
      render: (value) => t(value ? '是' : '否'),
    },
    {
      title: t('模型限制'),
      dataIndex: 'model_limits_enabled',
      width: 110,
      render: (value) => t(value ? '是' : '否'),
    },
    {
      title: t('过期时间'),
      dataIndex: 'expired_time',
      width: 180,
      render: (value) =>
        value === -1 ? t('永不过期') : formatValue(value, 'expired_time', t),
    },
    {
      title: t('操作'),
      dataIndex: 'operate',
      fixed: 'right',
      width: 110,
      render: (_, record) => (
        <Button
          size='small'
          type='primary'
          onClick={() => openEditToken(record)}
        >
          {t('编辑')}
        </Button>
      ),
    },
  ];

  return (
    <div className='space-y-4'>
      <SummaryGrid data={statistics} />
      <Card title='令牌列表' className='!rounded-lg'>
        <div className='flex flex-col xl:flex-row gap-3 mb-4'>
          <Select
            value={filters.status}
            style={{ width: 160 }}
            onChange={(value) => {
              const nextFilters = { ...filters, status: String(value) };
              setFilters(nextFilters);
              loadTokens(1, pageSize, nextFilters);
            }}
          >
            <Select.Option value='0'>{t('全部')}</Select.Option>
            <Select.Option value='1'>{t('启用')}</Select.Option>
            <Select.Option value='2'>{t('禁用')}</Select.Option>
            <Select.Option value='3'>{t('已过期')}</Select.Option>
            <Select.Option value='4'>{t('已耗尽')}</Select.Option>
          </Select>
          <Input
            value={filters.key}
            placeholder={t('搜索令牌 Key')}
            onChange={(value) =>
              setFilters((prev) => ({ ...prev, key: value }))
            }
            onEnterPress={() => loadTokens(1, pageSize)}
            className='xl:max-w-sm'
          />
          <Select
            value={filters.group}
            placeholder={t('筛选分组')}
            optionList={groups}
            renderOptionItem={renderGroupOption}
            filter={selectFilter}
            showClear
            onChange={(value) => {
              const nextFilters = { ...filters, group: value || '' };
              setFilters(nextFilters);
              loadTokens(1, pageSize, nextFilters);
            }}
            className='xl:max-w-xs'
            style={{ width: 180 }}
          />
          <Space>
            <Button type='primary' onClick={() => loadTokens(1, pageSize)}>
              {t('搜索')}
            </Button>
            <Button
              onClick={() => {
                const nextFilters = { status: '0', key: '', group: '' };
                setFilters(nextFilters);
                loadTokens(1, pageSize, nextFilters);
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
            onPageChange: (page) => loadTokens(page, pageSize),
            onPageSizeChange: (size) => {
              setPageSize(size);
              loadTokens(1, size);
            },
          }}
        />
      </Card>
      <SideSheet
        placement='right'
        title={
          <Space>
            <Tag color='blue' shape='circle'>
              {t('更新')}
            </Tag>
            <Title heading={4} style={{ margin: 0 }}>
              {t('更新令牌信息')}
            </Title>
          </Space>
        }
        bodyStyle={{ padding: 0 }}
        visible={Boolean(editingToken)}
        width={600}
        closeIcon={null}
        onCancel={() => {
          setEditingToken(null);
          setEditForm(null);
        }}
        footer={
          <div className='flex justify-end bg-semi-color-bg-0'>
            <Space>
              <Button
                theme='solid'
                type='primary'
                className='!rounded-lg'
                icon={<Save size={16} />}
                loading={saving}
                onClick={saveToken}
              >
                {t('提交')}
              </Button>
              <Button
                theme='light'
                type='primary'
                className='!rounded-lg'
                icon={<X size={16} />}
                onClick={() => {
                  setEditingToken(null);
                  setEditForm(null);
                }}
              >
                {t('取消')}
              </Button>
            </Space>
          </div>
        }
      >
        {editForm && (
          <Spin spinning={saving}>
            <div className='p-2 space-y-3'>
              <Card className='!rounded-2xl shadow-sm border-0'>
                <div className='flex items-center mb-3'>
                  <Avatar size='small' color='blue' className='mr-2 shadow-md'>
                    <KeyRound size={16} />
                  </Avatar>
                  <div>
                    <Text className='text-lg font-medium'>{t('基本信息')}</Text>
                    <div className='text-xs text-semi-color-text-2'>
                      {t('设置令牌的基本信息')}
                    </div>
                  </div>
                </div>
                <div className='space-y-3'>
                  <label className='space-y-1 block'>
                    <Text type='secondary'>{t('名称')}</Text>
                    <Input
                      value={editForm.name}
                      placeholder={t('请输入名称')}
                      showClear
                      onChange={(value) => patchEditForm({ name: value })}
                    />
                  </label>
                  <label className='space-y-1 block'>
                    <Text type='secondary'>{t('令牌分组')}</Text>
                    <Select
                      value={editForm.group}
                      placeholder={t('令牌分组，默认使用用户分组')}
                      optionList={groupOptions}
                      renderOptionItem={renderGroupOption}
                      filter={selectFilter}
                      showClear
                      style={{ width: '100%' }}
                      onChange={(value) =>
                        patchEditForm({ group: value || '' })
                      }
                    />
                  </label>
                  <div className='grid grid-cols-1 md:grid-cols-2 gap-3'>
                    <label className='space-y-1 block'>
                      <Text type='secondary'>{t('状态')}</Text>
                      <Select
                        value={editForm.status}
                        style={{ width: '100%' }}
                        onChange={(value) =>
                          patchEditForm({ status: Number(value) })
                        }
                      >
                        <Select.Option value={1}>{t('启用')}</Select.Option>
                        <Select.Option value={2}>{t('禁用')}</Select.Option>
                        <Select.Option value={3}>{t('已过期')}</Select.Option>
                        <Select.Option value={4}>{t('已耗尽')}</Select.Option>
                      </Select>
                    </label>
                    <label className='space-y-1 block'>
                      <Text type='secondary'>{t('过期时间戳')}</Text>
                      <InputNumber
                        value={editForm.expired_time}
                        style={{ width: '100%' }}
                        onChange={(value) =>
                          patchEditForm({ expired_time: value ?? -1 })
                        }
                      />
                      <Text type='tertiary' size='small'>
                        {t('-1 表示永不过期')}
                      </Text>
                    </label>
                  </div>
                  <div>
                    <Text type='secondary'>{t('过期时间快捷设置')}</Text>
                    <div className='mt-2'>
                      <Space wrap>
                        <Button
                          theme='light'
                          type='primary'
                          onClick={() => setTokenExpiration(0, 0, 0)}
                        >
                          {t('永不过期')}
                        </Button>
                        <Button
                          theme='light'
                          type='tertiary'
                          onClick={() => setTokenExpiration(1, 0, 0)}
                        >
                          {t('一个月')}
                        </Button>
                        <Button
                          theme='light'
                          type='tertiary'
                          onClick={() => setTokenExpiration(0, 1, 0)}
                        >
                          {t('一天')}
                        </Button>
                        <Button
                          theme='light'
                          type='tertiary'
                          onClick={() => setTokenExpiration(0, 0, 1)}
                        >
                          {t('一小时')}
                        </Button>
                      </Space>
                    </div>
                  </div>
                </div>
              </Card>

              <Card className='!rounded-2xl shadow-sm border-0'>
                <div className='flex items-center mb-3'>
                  <Avatar size='small' color='green' className='mr-2 shadow-md'>
                    <CreditCard size={16} />
                  </Avatar>
                  <div>
                    <Text className='text-lg font-medium'>{t('额度设置')}</Text>
                    <div className='text-xs text-semi-color-text-2'>
                      {t('设置令牌可用额度')}
                    </div>
                  </div>
                </div>
                <div className='space-y-3'>
                  <label className='space-y-1 block'>
                    <Text type='secondary'>{t('剩余金额')}</Text>
                    <InputNumber
                      min={0}
                      step={1}
                      prefix={
                        currency.type === 'TOKENS' ? undefined : currency.symbol
                      }
                      value={editForm.remain_amount}
                      disabled={editForm.unlimited_quota}
                      style={{ width: '100%' }}
                      onChange={(value) => {
                        const amount = value ?? 0;
                        patchEditForm({
                          remain_amount: amount,
                          remain_quota: displayAmountToQuota(amount),
                        });
                      }}
                    />
                  </label>
                  <div className='flex items-center justify-between gap-3 rounded-lg border border-semi-color-border px-3 py-2'>
                    <div>
                      <Text>{t('无限额度')}</Text>
                      <div className='text-xs text-semi-color-text-2'>
                        {t('令牌额度只限制令牌自身的最大额度使用量')}
                      </div>
                    </div>
                    <Switch
                      checked={editForm.unlimited_quota}
                      onChange={(checked) =>
                        patchEditForm({ unlimited_quota: checked })
                      }
                    />
                  </div>
                </div>
              </Card>

              <Card className='!rounded-2xl shadow-sm border-0'>
                <div className='flex items-center mb-3'>
                  <Avatar
                    size='small'
                    color='purple'
                    className='mr-2 shadow-md'
                  >
                    <Link2 size={16} />
                  </Avatar>
                  <div>
                    <Text className='text-lg font-medium'>{t('访问限制')}</Text>
                    <div className='text-xs text-semi-color-text-2'>
                      {t('设置令牌的访问限制')}
                    </div>
                  </div>
                </div>
                <div className='space-y-3'>
                  <label className='space-y-1 block'>
                    <Text type='secondary'>{t('模型限制列表')}</Text>
                    <Select
                      value={editForm.model_limits}
                      placeholder={t(
                        '请选择该令牌支持的模型，留空支持所有模型',
                      )}
                      optionList={modelOptions}
                      multiple
                      filter={selectFilter}
                      autoClearSearchValue={false}
                      searchPosition='dropdown'
                      showClear
                      style={{ width: '100%' }}
                      onChange={(value) =>
                        patchEditForm({ model_limits: value || [] })
                      }
                    />
                    <Text type='tertiary' size='small'>
                      {t('非必要，不建议启用模型限制')}
                    </Text>
                  </label>
                  <label className='space-y-1 block'>
                    <Text type='secondary'>
                      {t('IP 白名单（支持 CIDR 表达式）')}
                    </Text>
                    <TextArea
                      value={editForm.allow_ips}
                      placeholder={t('允许的 IP，一行一个，不填写则不限制')}
                      autosize
                      rows={2}
                      onChange={(value) => patchEditForm({ allow_ips: value })}
                    />
                    <Text type='tertiary' size='small'>
                      {t('请配合 nginx 或 CDN 等可信网关使用')}
                    </Text>
                  </label>
                </div>
              </Card>
            </div>
          </Spin>
        )}
      </SideSheet>
    </div>
  );
}

const RISK_WINDOW_OPTIONS = [
  { value: '24h', label: '最近 24 小时', amount: 24, unit: 'hour' },
  { value: '7d', label: '最近 7 天', amount: 7, unit: 'day' },
  { value: '30d', label: '最近 30 天', amount: 30, unit: 'day' },
  { value: 'custom', label: '自定义' },
];

const SHARED_IP_SORT_OPTIONS = [
  { value: '', label: '默认排序' },
  { value: 'user_count', label: '用户数' },
  { value: 'token_count', label: '令牌数' },
  { value: 'request_count', label: '请求数' },
  { value: 'error_count', label: '错误数' },
  { value: 'quota', label: '金额' },
  { value: 'first_seen_at', label: '首次出现' },
  { value: 'last_seen_at', label: '最后出现' },
];

const TOKEN_MULTI_IP_SORT_OPTIONS = [
  { value: '', label: '默认排序' },
  { value: 'ip_count', label: 'IP 数' },
  { value: 'request_count', label: '请求数' },
  { value: 'error_count', label: '错误数' },
  { value: 'quota', label: '金额' },
  { value: 'first_seen_at', label: '首次出现' },
  { value: 'last_seen_at', label: '最后出现' },
  { value: 'token_id', label: '令牌 ID' },
];

const EMPTY_PAGE = { items: [], total: 0, page: 1, page_size: 20 };

function getRiskWindowRange(filters) {
  if (filters.window === 'custom' && filters.range?.length === 2) {
    return {
      start: dayjs(filters.range[0]).unix(),
      end: dayjs(filters.range[1]).unix(),
    };
  }
  const option =
    RISK_WINDOW_OPTIONS.find((item) => item.value === filters.window) ||
    RISK_WINDOW_OPTIONS[0];
  const effectiveOption = option.amount ? option : RISK_WINDOW_OPTIONS[0];
  return {
    start: dayjs()
      .subtract(effectiveOption.amount, effectiveOption.unit)
      .unix(),
    end: dayjs().unix(),
  };
}

function compactRiskLabels(items, renderLabel, max = 4) {
  if (!items?.length) return '-';
  const visible = items.slice(0, max);
  return (
    <div className='flex flex-wrap gap-1'>
      {visible.map((item, index) => (
        <Tag key={`${renderLabel(item)}-${index}`} size='small'>
          {renderLabel(item)}
        </Tag>
      ))}
      {items.length > max && <Tag size='small'>+{items.length - max}</Tag>}
    </div>
  );
}

function RiskPanel({ data }) {
  const { t } = useTranslation();
  const currency = getCurrencyConfig();
  const [coverage, setCoverage] = useState(data?.coverage || {});
  const [sharedIPs, setSharedIPs] = useState(data?.sharedIPs || EMPTY_PAGE);
  const [tokenMultiIPs, setTokenMultiIPs] = useState(
    data?.tokenMultiIPs || EMPTY_PAGE,
  );
  const [filters, setFilters] = useState({
    window: '24h',
    range: [],
    keyword: '',
  });
  const [sharedSort, setSharedSort] = useState({ sort: '', order: 'desc' });
  const [tokenSort, setTokenSort] = useState({ sort: '', order: 'desc' });
  const [sharedPageSize, setSharedPageSize] = useState(
    data?.sharedIPs?.page_size || 20,
  );
  const [tokenPageSize, setTokenPageSize] = useState(
    data?.tokenMultiIPs?.page_size || 20,
  );
  const [coverageLoading, setCoverageLoading] = useState(false);
  const [sharedLoading, setSharedLoading] = useState(false);
  const [tokenLoading, setTokenLoading] = useState(false);
  const [applying, setApplying] = useState(false);

  useEffect(() => {
    setCoverage(data?.coverage || {});
    setSharedIPs(data?.sharedIPs || EMPTY_PAGE);
    setTokenMultiIPs(data?.tokenMultiIPs || EMPTY_PAGE);
    setSharedPageSize(data?.sharedIPs?.page_size || 20);
    setTokenPageSize(data?.tokenMultiIPs?.page_size || 20);
  }, [data]);

  const riskParams = (page, pageSize, nextFilters, nextSort) => {
    const range = getRiskWindowRange(nextFilters);
    const params = {
      p: page,
      page_size: pageSize,
      start: range.start,
      end: range.end,
      order: nextSort.order,
    };
    if (nextSort.sort) params.sort = nextSort.sort;
    if (nextFilters.keyword?.trim()) {
      params.keyword = nextFilters.keyword.trim();
    }
    return params;
  };

  const loadCoverage = async () => {
    setCoverageLoading(true);
    try {
      const nextCoverage = await API.get(
        '/api/enhancements/risk/ip-log-coverage',
      ).then(unwrap);
      setCoverage(nextCoverage || {});
    } catch (error) {
      showError(error.message || error);
    } finally {
      setCoverageLoading(false);
    }
  };

  const loadSharedIPs = async (
    page = 1,
    pageSize = sharedPageSize,
    nextFilters = filters,
    nextSort = sharedSort,
  ) => {
    setSharedLoading(true);
    try {
      const nextData = await API.get(
        '/api/enhancements/risk/shared-token-ips',
        { params: riskParams(page, pageSize, nextFilters, nextSort) },
      ).then(unwrap);
      setSharedIPs(nextData || EMPTY_PAGE);
    } catch (error) {
      showError(error.message || error);
    } finally {
      setSharedLoading(false);
    }
  };

  const loadTokenMultiIPs = async (
    page = 1,
    pageSize = tokenPageSize,
    nextFilters = filters,
    nextSort = tokenSort,
  ) => {
    setTokenLoading(true);
    try {
      const nextData = await API.get('/api/enhancements/risk/token-multi-ips', {
        params: riskParams(page, pageSize, nextFilters, nextSort),
      }).then(unwrap);
      setTokenMultiIPs(nextData || EMPTY_PAGE);
    } catch (error) {
      showError(error.message || error);
    } finally {
      setTokenLoading(false);
    }
  };

  const refreshRiskDetails = async (nextFilters = filters) => {
    await Promise.all([
      loadCoverage(),
      loadSharedIPs(1, sharedPageSize, nextFilters),
      loadTokenMultiIPs(1, tokenPageSize, nextFilters),
    ]);
  };

  const enableAll = () => {
    Modal.confirm({
      title: t('一键开启 IP 日志记录'),
      content: t('确认将所有未开启“记录请求与错误日志IP”的用户改为开启？'),
      okText: t('开启'),
      cancelText: t('取消'),
      onOk: async () => {
        setApplying(true);
        try {
          const res = await API.post(
            '/api/enhancements/risk/ip-log/enable-all',
          );
          const result = unwrap(res);
          setCoverage(result?.coverage || {});
          showSuccess(t('操作成功'));
          await loadCoverage();
        } catch (error) {
          showError(error.message || error);
        } finally {
          setApplying(false);
        }
      },
    });
  };

  const totalUsers = coverage?.total_users || 0;
  const enabledUsers = coverage?.enabled_users || 0;
  const disabledUsers = coverage?.disabled_users || 0;

  const sharedColumns = [
    {
      title: 'IP',
      dataIndex: 'ip',
      fixed: 'left',
      width: 150,
    },
    {
      title: t('令牌数'),
      dataIndex: 'token_count',
      render: (value) => formatNumber(value),
    },
    {
      title: t('用户数'),
      dataIndex: 'user_count',
      render: (value) => formatNumber(value),
    },
    {
      title: t('请求数'),
      dataIndex: 'request_count',
      render: (value) => formatNumber(value),
    },
    {
      title: t('错误数'),
      dataIndex: 'error_count',
      render: (value) => formatNumber(value),
    },
    {
      title: t('金额'),
      dataIndex: 'quota',
      render: (value) => formatDisplayAmount(value, currency),
    },
    {
      title: t('首次出现'),
      dataIndex: 'first_seen_at',
      render: (value) => formatValue(value, 'first_seen_at', t),
    },
    {
      title: t('最后出现'),
      dataIndex: 'last_seen_at',
      render: (value) => formatValue(value, 'last_seen_at', t),
    },
    {
      title: t('用户'),
      dataIndex: 'users',
      width: 260,
      render: (users) =>
        compactRiskLabels(
          users,
          (user) => `${user.username || '-'} (#${user.user_id})`,
          3,
        ),
    },
    {
      title: t('令牌'),
      dataIndex: 'tokens',
      width: 300,
      render: (tokens) =>
        compactRiskLabels(
          tokens,
          (token) =>
            `${token.token_name || '-'} (#${token.token_id}, U${token.user_id})`,
          3,
        ),
    },
  ];

  const tokenColumns = [
    {
      title: t('令牌 ID'),
      dataIndex: 'token_id',
      fixed: 'left',
      width: 100,
    },
    {
      title: t('令牌名称'),
      dataIndex: 'token_name',
      width: 180,
      render: (value) => value || '-',
    },
    {
      title: t('用户 ID'),
      dataIndex: 'user_id',
      width: 100,
    },
    {
      title: t('用户名'),
      dataIndex: 'username',
      width: 140,
      render: (value) => value || '-',
    },
    {
      title: t('IP 数'),
      dataIndex: 'ip_count',
      render: (value) => formatNumber(value),
    },
    {
      title: t('请求数'),
      dataIndex: 'request_count',
      render: (value) => formatNumber(value),
    },
    {
      title: t('错误数'),
      dataIndex: 'error_count',
      render: (value) => formatNumber(value),
    },
    {
      title: t('金额'),
      dataIndex: 'quota',
      render: (value) => formatDisplayAmount(value, currency),
    },
    {
      title: t('首次出现'),
      dataIndex: 'first_seen_at',
      render: (value) => formatValue(value, 'first_seen_at', t),
    },
    {
      title: t('最后出现'),
      dataIndex: 'last_seen_at',
      render: (value) => formatValue(value, 'last_seen_at', t),
    },
    {
      title: 'IP',
      dataIndex: 'ips',
      width: 300,
      render: (ips) => compactRiskLabels(ips, (ip) => ip, 5),
    },
  ];

  const renderSortControls = (sortState, options, onChange) => (
    <Space wrap>
      <Select
        value={sortState.sort}
        size='small'
        style={{ width: 140 }}
        onChange={(value) => onChange({ ...sortState, sort: value })}
      >
        {options.map((option) => (
          <Select.Option key={option.value} value={option.value}>
            {t(option.label)}
          </Select.Option>
        ))}
      </Select>
      <Select
        value={sortState.order}
        size='small'
        style={{ width: 110 }}
        onChange={(value) => onChange({ ...sortState, order: value })}
      >
        <Select.Option value='desc'>{t('降序')}</Select.Option>
        <Select.Option value='asc'>{t('升序')}</Select.Option>
      </Select>
    </Space>
  );

  return (
    <div className='space-y-4'>
      <Card title={t('IP 日志记录覆盖率')} className='!rounded-lg'>
        <Spin spinning={coverageLoading}>
          <div className='flex flex-col md:flex-row md:items-end md:justify-between gap-4'>
            <div>
              <Text type='secondary'>
                {t('已开启记录请求与错误日志IP的用户占比')}
              </Text>
              <div className='text-4xl font-semibold mt-2 text-semi-color-text-0'>
                {formatPercent(coverage?.enabled_ratio)}
              </div>
              <div className='mt-2 text-semi-color-text-1'>
                {formatNumber(enabledUsers)} / {formatNumber(totalUsers)}
              </div>
            </div>
            <div className='grid grid-cols-2 gap-3 min-w-64'>
              <div className='rounded-lg border border-semi-color-border p-3'>
                <Text type='secondary' size='small'>
                  {t('已开启用户')}
                </Text>
                <div className='text-xl font-semibold mt-1'>
                  {formatNumber(enabledUsers)}
                </div>
              </div>
              <div className='rounded-lg border border-semi-color-border p-3'>
                <Text type='secondary' size='small'>
                  {t('未开启用户')}
                </Text>
                <div className='text-xl font-semibold mt-1'>
                  {formatNumber(disabledUsers)}
                </div>
              </div>
            </div>
          </div>
          <div className='mt-4'>
            <Button
              size='small'
              type='primary'
              loading={applying}
              disabled={disabledUsers === 0}
              onClick={enableAll}
            >
              {t('一键开启未开启用户')}
            </Button>
          </div>
        </Spin>
      </Card>

      <Card className='!rounded-lg'>
        <div className='flex flex-col xl:flex-row gap-3 xl:items-end'>
          <label className='space-y-1'>
            <Text type='secondary' size='small'>
              {t('时间范围')}
            </Text>
            <Select
              value={filters.window}
              style={{ width: 160 }}
              onChange={(value) => {
                const nextFilters = {
                  ...filters,
                  window: value,
                  range: value === 'custom' ? filters.range : [],
                };
                setFilters(nextFilters);
                if (value !== 'custom') {
                  refreshRiskDetails(nextFilters);
                }
              }}
            >
              {RISK_WINDOW_OPTIONS.map((option) => (
                <Select.Option key={option.value} value={option.value}>
                  {t(option.label)}
                </Select.Option>
              ))}
            </Select>
          </label>
          <label className='space-y-1 flex-1 min-w-72'>
            <Text type='secondary' size='small'>
              {t('自定义时间')}
            </Text>
            <DatePicker
              className='w-full'
              type='dateTimeRange'
              value={filters.range}
              inputReadOnly
              showClear
              disabled={filters.window !== 'custom'}
              placeholder={[t('开始时间'), t('结束时间')]}
              onChange={(value) =>
                setFilters((prev) => ({
                  ...prev,
                  window: 'custom',
                  range: value || [],
                }))
              }
            />
          </label>
          <label className='space-y-1 flex-1 min-w-60'>
            <Text type='secondary' size='small'>
              {t('关键词')}
            </Text>
            <Input
              value={filters.keyword}
              placeholder={t('搜索 IP、用户名、用户 ID 或令牌')}
              onChange={(value) =>
                setFilters((prev) => ({ ...prev, keyword: value }))
              }
              onEnterPress={() => refreshRiskDetails(filters)}
            />
          </label>
          <Button
            type='primary'
            icon={<RefreshCw size={16} />}
            loading={coverageLoading || sharedLoading || tokenLoading}
            onClick={() => refreshRiskDetails(filters)}
          >
            {t('刷新')}
          </Button>
        </div>
      </Card>

      <Card title={t('多令牌共用 IP')} className='!rounded-lg'>
        <div className='flex justify-end mb-3'>
          {renderSortControls(
            sharedSort,
            SHARED_IP_SORT_OPTIONS,
            (nextSort) => {
              setSharedSort(nextSort);
              loadSharedIPs(1, sharedPageSize, filters, nextSort);
            },
          )}
        </div>
        <Table
          size='small'
          columns={sharedColumns}
          dataSource={(sharedIPs?.items || []).map((row) => ({
            ...row,
            _rowKey: row.ip,
          }))}
          rowKey='_rowKey'
          loading={sharedLoading}
          empty={<Empty description={t('暂无数据')} />}
          scroll={{ x: 'max-content' }}
          pagination={{
            currentPage: sharedIPs?.page || 1,
            pageSize: sharedPageSize,
            total: sharedIPs?.total || 0,
            showSizeChanger: true,
            pageSizeOptions: [10, 20, 50, 100],
            onPageChange: (page) => loadSharedIPs(page, sharedPageSize),
            onPageSizeChange: (size) => {
              setSharedPageSize(size);
              loadSharedIPs(1, size);
            },
          }}
        />
      </Card>

      <Card title={t('单令牌多 IP')} className='!rounded-lg'>
        <div className='flex justify-end mb-3'>
          {renderSortControls(
            tokenSort,
            TOKEN_MULTI_IP_SORT_OPTIONS,
            (nextSort) => {
              setTokenSort(nextSort);
              loadTokenMultiIPs(1, tokenPageSize, filters, nextSort);
            },
          )}
        </div>
        <Table
          size='small'
          columns={tokenColumns}
          dataSource={(tokenMultiIPs?.items || []).map((row) => ({
            ...row,
            _rowKey: row.token_id,
          }))}
          rowKey='_rowKey'
          loading={tokenLoading}
          empty={<Empty description={t('暂无数据')} />}
          scroll={{ x: 'max-content' }}
          pagination={{
            currentPage: tokenMultiIPs?.page || 1,
            pageSize: tokenPageSize,
            total: tokenMultiIPs?.total || 0,
            showSizeChanger: true,
            pageSizeOptions: [10, 20, 50, 100],
            onPageChange: (page) => loadTokenMultiIPs(page, tokenPageSize),
            onPageSizeChange: (size) => {
              setTokenPageSize(size);
              loadTokenMultiIPs(1, size);
            },
          }}
        />
      </Card>
    </div>
  );
}

function ModelStatusWindowSelect({ value, onChange, className = '' }) {
  const { t } = useTranslation();
  return (
    <Select
      value={value}
      onChange={onChange}
      className={className || 'w-40'}
      optionList={MODEL_STATUS_WINDOWS.map((item) => ({
        value: item.value,
        label: t(item.label),
      }))}
    />
  );
}

function ModelStatusStat({ icon: Icon, label, value, hint }) {
  return (
    <Card className='!rounded-lg'>
      <div className='flex items-center justify-between gap-3'>
        <div>
          <div className='text-xs text-semi-color-text-2'>{label}</div>
          <div className='mt-1 text-2xl font-semibold text-semi-color-text-0'>
            {value}
          </div>
          {hint ? (
            <div className='mt-1 text-xs text-semi-color-text-2'>{hint}</div>
          ) : null}
        </div>
        <div className='h-10 w-10 rounded-lg bg-semi-color-fill-0 flex items-center justify-center text-semi-color-text-2'>
          <Icon size={20} />
        </div>
      </div>
    </Card>
  );
}

function ModelStatusTimeline({ status }) {
  const { t } = useTranslation();
  const slots = Array.isArray(status?.slot_data) ? status.slot_data : [];
  const groupName = status?.group_name || status?.group || 'default';
  const modelName = status?.model_name || '-';

  return (
    <div className='space-y-1.5'>
      <div className='flex h-6 w-full items-stretch gap-[3px] overflow-hidden rounded-sm'>
        {slots.length > 0 ? (
          slots.map((slot) => {
            const slotMeta = getModelStatusMeta(slot.status);
            const title = `${dayjs.unix(slot.start_time).format('MM-DD HH:mm')} - ${dayjs
              .unix(slot.end_time)
              .format('MM-DD HH:mm')} · ${formatStatusPercent(
              slot.success_rate,
            )} · ${formatNumber(Number(slot.total_requests || 0))}`;
            return (
              <div
                key={`${groupName}-${modelName}-${slot.slot}`}
                className={`min-w-[3px] flex-1 rounded-[1px] ${slotMeta.barClass}`}
                title={title}
              />
            );
          })
        ) : (
          <div className='h-full flex-1 rounded-sm bg-semi-color-fill-1' />
        )}
      </div>
      <div className='flex items-center justify-between text-[10px] uppercase tracking-wide text-semi-color-text-2'>
        <span>{t('过去')}</span>
        <span>{t('现在')}</span>
      </div>
    </div>
  );
}

function ModelStatusCard({ status }) {
  const { t } = useTranslation();
  const meta = getModelStatusMeta(status?.current_status);
  const Icon = meta.icon;
  const groupName = status?.group_name || status?.group || 'default';
  const modelName = status?.model_name || '-';

  return (
    <Card className='!rounded-lg'>
      <div className='flex flex-col gap-4'>
        <div className='flex items-start justify-between gap-3'>
          <div className='min-w-0'>
            <div className='truncate text-base font-semibold text-semi-color-text-0'>
              {status?.display_name || modelName}
            </div>
            <div className='mt-2 grid grid-cols-1 gap-1 text-xs text-semi-color-text-2 sm:grid-cols-2'>
              <div className='truncate'>
                {t('分组')}：{groupName}
              </div>
              <div className='truncate'>
                {t('模型')}：{modelName}
              </div>
            </div>
            <div className='mt-1 text-xs text-semi-color-text-2'>
              {formatNumber(Number(status?.total_requests || 0))} {t('总请求')}
            </div>
          </div>
          <Tag color={meta.color}>
            <span className='inline-flex items-center gap-1'>
              <Icon size={14} />
              {t(meta.label)}
            </span>
          </Tag>
        </div>

        <div className='grid grid-cols-3 gap-2 text-sm'>
          <div>
            <div className='text-xs text-semi-color-text-2'>{t('成功率')}</div>
            <div className='mt-1 font-medium'>
              {formatStatusPercent(status?.success_rate)}
            </div>
          </div>
          <div>
            <div className='text-xs text-semi-color-text-2'>{t('成功')}</div>
            <div className='mt-1 font-medium'>
              {formatNumber(Number(status?.success_count || 0))}
            </div>
          </div>
          <div>
            <div className='text-xs text-semi-color-text-2'>{t('错误')}</div>
            <div className='mt-1 font-medium'>
              {formatNumber(Number(status?.error_count || 0))}
            </div>
          </div>
        </div>

        <ModelStatusTimeline status={status} />
      </div>
    </Card>
  );
}

function ModelStatusBoard({
  statuses,
  loading,
  windowValue,
  onWindowChange,
  lastUpdated,
  toolbar,
  extraControls,
  showWindowSelect = true,
}) {
  const { t } = useTranslation();
  const items = Array.isArray(statuses) ? statuses : [];
  const overview = modelStatusOverview(items);

  return (
    <div className='space-y-4'>
      <div className='flex flex-col gap-3 md:flex-row md:items-center md:justify-between'>
        <div>
          <Title heading={4} className='!mb-1'>
            {t('全站模型状态')}
          </Title>
          <Text type='tertiary'>
            {t('最后更新')}:{' '}
            {lastUpdated
              ? dayjs(lastUpdated).format('YYYY-MM-DD HH:mm:ss')
              : '-'}
          </Text>
        </div>
        <Space wrap>
          {extraControls}
          {toolbar}
          {showWindowSelect ? (
            <ModelStatusWindowSelect
              value={windowValue}
              onChange={onWindowChange}
            />
          ) : null}
        </Space>
      </div>

      <div className='grid grid-cols-1 gap-3 md:grid-cols-4'>
        <ModelStatusStat
          icon={LineChart}
          label={t('模型数量')}
          value={formatNumber(overview.totalModels)}
        />
        <ModelStatusStat
          icon={Activity}
          label={t('总请求')}
          value={formatNumber(overview.totalRequests)}
        />
        <ModelStatusStat
          icon={CheckCircle2}
          label={t('平均成功率')}
          value={formatStatusPercent(overview.successRate)}
        />
        <ModelStatusStat
          icon={AlertTriangle}
          label={t('异常模型')}
          value={formatNumber(
            Number(overview.statusCounts.yellow || 0) +
              Number(overview.statusCounts.red || 0),
          )}
          hint={`${t('正常')} ${formatNumber(
            overview.statusCounts.green || 0,
          )}`}
        />
      </div>

      <Spin spinning={loading}>
        {items.length > 0 ? (
          <div className='grid grid-cols-1 gap-3 xl:grid-cols-2'>
            {items.map((item) => (
              <ModelStatusCard
                key={`${item.group || item.group_name || 'default'}:${item.model_name}`}
                status={item}
              />
            ))}
          </div>
        ) : (
          <Card className='!rounded-lg'>
            <Empty description={t('暂无模型状态数据')} />
          </Card>
        )}
      </Spin>
    </div>
  );
}

function ModelStatusPanel({ data }) {
  const { t } = useTranslation();
  const [config, setConfig] = useState(data?.config || {});
  const [statuses, setStatuses] = useState(data?.statuses || []);
  const [windowValue, setWindowValue] = useState(getModelStatusConfigWindow(data?.config));
  const [publicEnabled, setPublicEnabled] = useState(!!data?.config?.public_embed_enabled);
  const [refreshMinutes, setRefreshMinutes] = useState(
    getModelStatusRefreshMinutes(data?.config),
  );
  const [slotMinutes, setSlotMinutes] = useState(
    getModelStatusSlotMinutes(data?.config),
  );
  const [greenThreshold, setGreenThreshold] = useState(
    getModelStatusThreshold(data?.config, 'green_threshold', 95),
  );
  const [yellowThreshold, setYellowThreshold] = useState(
    getModelStatusThreshold(data?.config, 'yellow_threshold', 80),
  );
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [lastUpdated, setLastUpdated] = useState(null);
  const publicUrl = getModelStatusPublicUrl(config);

  const loadStatuses = async (nextWindow = windowValue) => {
    setLoading(true);
    try {
      const nextStatuses = await API.get(
        '/api/enhancements/model-status/status/all',
        { params: { window: nextWindow } },
      ).then(unwrap);
      setStatuses(nextStatuses || []);
      setLastUpdated(new Date());
    } catch (error) {
      showError(error.message);
    } finally {
      setLoading(false);
    }
  };

  const loadConfig = async () => {
    try {
      const nextConfig = await API.get(
        '/api/enhancements/model-status/config/time-window',
      ).then(unwrap);
      setConfig(nextConfig || {});
    } catch (error) {
      showError(error.message);
    }
  };

  useEffect(() => {
    const nextConfig = data?.config || {};
    setConfig(nextConfig);
    setStatuses(data?.statuses || []);
    setWindowValue(getModelStatusConfigWindow(nextConfig));
    setPublicEnabled(!!nextConfig.public_embed_enabled);
    setRefreshMinutes(getModelStatusRefreshMinutes(nextConfig));
    setSlotMinutes(getModelStatusSlotMinutes(nextConfig));
    setGreenThreshold(
      getModelStatusThreshold(nextConfig, 'green_threshold', 95),
    );
    setYellowThreshold(
      getModelStatusThreshold(nextConfig, 'yellow_threshold', 80),
    );
    setLastUpdated(data?.statuses ? new Date() : null);
  }, [data]);

  const handleSaveSettings = async () => {
    setSaving(true);
    try {
      const minutes = Math.min(1440, Math.max(1, Number(refreshMinutes || 1)));
      const nextSlotMinutes = Math.min(
        1440,
        Math.max(5, Number(slotMinutes || 30)),
      );
      const nextGreenThreshold = Math.min(
        100,
        Math.max(1, Number(greenThreshold || 95)),
      );
      const nextYellowThreshold = Math.min(
        100,
        Math.max(1, Number(yellowThreshold || 80)),
      );
      if (nextGreenThreshold < nextYellowThreshold) {
        showError(t('绿色阈值不能低于黄色阈值'));
        return;
      }
      await Promise.all([
        API.put('/api/enhancements/model-status/config/public-embed', {
          value: publicEnabled,
        }).then(unwrap),
        API.put('/api/enhancements/model-status/config/time-window', {
          value: windowValue,
        }).then(unwrap),
        API.put('/api/enhancements/model-status/config/refresh-interval', {
          value: minutes * 60,
        }).then(unwrap),
        API.put('/api/enhancements/model-status/config/slot-granularity', {
          value: nextSlotMinutes,
        }).then(unwrap),
        API.put('/api/enhancements/model-status/config/threshold-green', {
          value: nextGreenThreshold,
        }).then(unwrap),
        API.put('/api/enhancements/model-status/config/threshold-yellow', {
          value: nextYellowThreshold,
        }).then(unwrap),
      ]);
      showSuccess(t('配置已保存'));
      await loadConfig();
      await loadStatuses(windowValue);
    } catch (error) {
      showError(error.message);
    } finally {
      setSaving(false);
    }
  };

  const handleCopy = async () => {
    if (await copy(publicUrl)) {
      showSuccess(t('复制成功'));
    }
  };

  return (
    <div className='space-y-4'>
      <Card className='!rounded-lg'>
        <div className='flex flex-col gap-4'>
          <div className='flex flex-col gap-3 md:flex-row md:items-center md:justify-between'>
            <div className='flex items-center gap-3'>
              <div className='h-10 w-10 rounded-lg bg-blue-50 text-blue-600 flex items-center justify-center'>
                <Globe2 size={20} />
              </div>
              <div>
                <div className='text-base font-semibold text-semi-color-text-0'>
                  {t('公开嵌入')}
                </div>
                <div className='text-sm text-semi-color-text-2'>
                  {t('开启后外部用户可以访问整个站的模型状态页面')}
                </div>
              </div>
            </div>
            <Switch
              checked={publicEnabled}
              onChange={setPublicEnabled}
              checkedText={t('开启')}
              uncheckedText={t('关闭')}
            />
          </div>

          <div className='grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-5'>
            <label className='space-y-1'>
              <Text type='secondary'>{t('时间范围')}</Text>
              <ModelStatusWindowSelect
                value={windowValue}
                onChange={setWindowValue}
                className='w-full'
              />
            </label>
            <label className='space-y-1'>
              <Text type='secondary'>{t('刷新间隔（分钟）')}</Text>
              <InputNumber
                min={1}
                max={1440}
                value={refreshMinutes}
                onChange={(value) => setRefreshMinutes(value || 1)}
                style={{ width: '100%' }}
              />
            </label>
            <label className='space-y-1'>
              <Text type='secondary'>{t('状态粒度（分钟）')}</Text>
              <InputNumber
                min={5}
                max={1440}
                value={slotMinutes}
                onChange={(value) => setSlotMinutes(value || 30)}
                style={{ width: '100%' }}
              />
            </label>
            <label className='space-y-1'>
              <Text type='secondary'>{t('绿色阈值（%）')}</Text>
              <InputNumber
                min={1}
                max={100}
                precision={1}
                value={greenThreshold}
                onChange={(value) => setGreenThreshold(value || 95)}
                style={{ width: '100%' }}
              />
            </label>
            <label className='space-y-1'>
              <Text type='secondary'>{t('黄色阈值（%）')}</Text>
              <InputNumber
                min={1}
                max={100}
                precision={1}
                value={yellowThreshold}
                onChange={(value) => setYellowThreshold(value || 80)}
                style={{ width: '100%' }}
              />
            </label>
          </div>

          <div className='grid grid-cols-1 gap-3 lg:grid-cols-[1fr_auto]'>
            <Input
              readOnly
              value={publicUrl}
              prefix={<Link2 size={16} />}
              addonBefore={t('公开访问地址')}
            />
            <Space>
              <Button
                icon={<CopyIcon size={16} />}
                onClick={handleCopy}
                disabled={!publicEnabled}
              >
                {t('复制地址')}
              </Button>
              <Button
                icon={<ExternalLink size={16} />}
                onClick={() => window.open(publicUrl, '_blank', 'noopener')}
                disabled={!publicEnabled}
              >
                {t('打开页面')}
              </Button>
              <Button
                type='primary'
                icon={<Save size={16} />}
                loading={saving}
                onClick={handleSaveSettings}
              >
                {t('保存设置')}
              </Button>
            </Space>
          </div>
        </div>
      </Card>

      <ModelStatusBoard
        statuses={statuses}
        loading={loading}
        windowValue={windowValue}
        onWindowChange={setWindowValue}
        lastUpdated={lastUpdated}
        showWindowSelect={false}
        toolbar={
          <Button
            icon={<RefreshCw size={16} />}
            loading={loading}
            onClick={() => loadStatuses(windowValue)}
          >
            {t('刷新')}
          </Button>
        }
      />
    </div>
  );
}

export function ModelStatusPublicPage() {
  const { t } = useTranslation();
  const [config, setConfig] = useState(null);
  const [statuses, setStatuses] = useState([]);
  const [groupFilter, setGroupFilter] = useState('');
  const [sortMode, setSortMode] = useState('requests_desc');
  const [loading, setLoading] = useState(false);
  const [available, setAvailable] = useState(true);
  const [lastUpdated, setLastUpdated] = useState(null);

  const loadPublicStatus = useCallback(async () => {
    setLoading(true);
    try {
      const [nextConfig, nextStatuses] = await Promise.all([
        API.get('/api/enhancements/model-status/embed/config').then(unwrap),
        API.get('/api/enhancements/model-status/embed/status/all').then(unwrap),
      ]);
      setConfig(nextConfig || {});
      setStatuses(nextStatuses || []);
      setAvailable(true);
      setLastUpdated(new Date());
    } catch (error) {
      setAvailable(false);
      setConfig(null);
      setStatuses([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadPublicStatus();
  }, [loadPublicStatus]);

  useEffect(() => {
    if (!available || !config) return undefined;
    const intervalMs = getModelStatusRefreshMinutes(config) * 60 * 1000;
    const timer = window.setInterval(() => {
      loadPublicStatus();
    }, intervalMs);
    return () => window.clearInterval(timer);
  }, [available, config, loadPublicStatus]);

  const groupOptions = useMemo(() => {
    const groups = Array.from(
      new Set(
        statuses
          .map((item) => item.group_name || item.group || 'default')
          .filter(Boolean),
      ),
    ).sort();
    return [
      { label: t('全部分组'), value: '' },
      ...groups.map((group) => ({ label: group, value: group })),
    ];
  }, [statuses, t]);

  const visibleStatuses = useMemo(() => {
    const items = statuses.filter((item) => {
      if (!groupFilter) return true;
      return (item.group_name || item.group || 'default') === groupFilter;
    });
    return [...items].sort((a, b) => {
      if (sortMode === 'success_rate_asc') {
        const rateDiff = Number(a.success_rate || 0) - Number(b.success_rate || 0);
        if (rateDiff !== 0) return rateDiff;
        return Number(b.total_requests || 0) - Number(a.total_requests || 0);
      }
      const requestDiff =
        Number(b.total_requests || 0) - Number(a.total_requests || 0);
      if (requestDiff !== 0) return requestDiff;
      return String(a.model_name || '').localeCompare(String(b.model_name || ''));
    });
  }, [groupFilter, sortMode, statuses]);

  if (!available && !loading) {
    return (
      <div className='min-h-screen bg-[#f6f8fb] px-4 py-10'>
        <div className='mx-auto max-w-3xl'>
          <Card className='!rounded-lg'>
            <Empty
              image={<Globe2 size={44} />}
              title={t('模型状态暂未公开')}
              description={t('管理员未开启公开嵌入')}
            />
          </Card>
        </div>
      </div>
    );
  }

  return (
    <div className='min-h-screen bg-[#f6f8fb] px-4 py-6 md:py-8'>
      <div className='mx-auto max-w-6xl space-y-5'>
        <ModelStatusBoard
          statuses={visibleStatuses}
          loading={loading}
          windowValue={getModelStatusConfigWindow(config || {})}
          onWindowChange={() => {}}
          lastUpdated={lastUpdated}
          showWindowSelect={false}
          extraControls={
            <>
              <Select
                value={groupFilter}
                onChange={(value) => setGroupFilter(value || '')}
                optionList={groupOptions}
                className='w-40'
              />
              <Select
                value={sortMode}
                onChange={setSortMode}
                optionList={MODEL_STATUS_SORT_OPTIONS.map((item) => ({
                  value: item.value,
                  label: t(item.label),
                }))}
                className='w-44'
              />
            </>
          }
        />
      </div>
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
  if (section === 'tokens') {
    return <TokensPanel data={data} />;
  }
  if (section === 'risk') {
    return <RiskPanel data={data} />;
  }
  if (section === 'model-status') {
    return <ModelStatusPanel data={data} />;
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
      const range = getRiskWindowRange({ window: '24h', range: [] });
      const riskParams = {
        p: 1,
        page_size: 20,
        start: range.start,
        end: range.end,
      };
      const [coverage, sharedIPs, tokenMultiIPs] = await Promise.all([
        API.get('/api/enhancements/risk/ip-log-coverage').then(unwrap),
        API.get('/api/enhancements/risk/shared-token-ips', {
          params: riskParams,
        }).then(unwrap),
        API.get('/api/enhancements/risk/token-multi-ips', {
          params: riskParams,
        }).then(unwrap),
      ]);
      return { coverage, sharedIPs, tokenMultiIPs };
    }
    case 'model-status': {
      const config = await API.get(
        '/api/enhancements/model-status/config/time-window',
      ).then(unwrap);
      const statuses = await API.get('/api/enhancements/model-status/status/all', {
        params: { window: getModelStatusConfigWindow(config) },
      }).then(unwrap);
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
