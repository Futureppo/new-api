import React, { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Card,
  Empty,
  Modal,
  SideSheet,
  Space,
  Switch,
  Table,
  Tabs,
  TabPane,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import {
  API,
  isRoot,
  showError,
  showSuccess,
  timestamp2string,
} from '../../../helpers';
import { isDisableDurationValid } from '../../../components/common/UserDisableInfo';

const unwrap = (res) => {
  if (!res.data?.success) throw new Error(res.data?.message || '请求失败');
  return res.data.data;
};
const EMPTY = { items: [], total: 0, page: 1, page_size: 20 };
const DEFAULT_QUERY = { sort: '', order: 'desc', filters: {} };
const SESSION_KIND = 'session';
const CACHE_KIND = 'cache';

export function SessionRiskSettings({ refreshKey }) {
  const { t } = useTranslation();
  const [enabled, setEnabled] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [saving, setSaving] = useState(false);
  useEffect(() => {
    let active = true;
    API.get('/api/enhancements/risk/session-settings')
      .then(unwrap)
      .then((data) => {
        if (active) {
          setEnabled(data.recording_enabled);
          setLoaded(true);
        }
      })
      .catch((err) => {
        if (active) showError(err.message);
      });
    return () => {
      active = false;
    };
  }, [refreshKey]);
  const save = async (value) => {
    setSaving(true);
    try {
      const data = await API.put('/api/enhancements/risk/session-settings', {
        recording_enabled: value,
      }).then(unwrap);
      setEnabled(data.recording_enabled);
      showSuccess(t('保存成功'));
    } catch (err) {
      showError(err.message);
    } finally {
      setSaving(false);
    }
  };
  return (
    <Card className='!rounded-lg'>
      <Space>
        <Typography.Text strong>{t('记录会话标识')}</Typography.Text>
        <Switch
          aria-label={t('记录会话标识')}
          checked={enabled}
          disabled={!isRoot() || !loaded || saving}
          loading={saving}
          onChange={save}
        />
        <Typography.Text type='secondary'>
          {t(
            '默认关闭；开启后记录新请求的会话 ID 和缓存关联标识，关闭后保留历史记录。',
          )}
        </Typography.Text>
      </Space>
      {!isRoot() && (
        <div className='mt-2 text-semi-color-text-2'>
          {t('仅超级管理员可修改此开关')}
        </div>
      )}
    </Card>
  );
}

function SessionRiskTable({
  kind,
  query,
  refreshKey,
  tableTools,
  BanConfirmContent,
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [page, setPage] = useState(EMPTY);
  const [pageSize, setPageSize] = useState(20);
  const [tableQuery, setTableQuery] = useState(DEFAULT_QUERY);
  const [loading, setLoading] = useState(false);
  const [selected, setSelected] = useState(null);
  const [banning, setBanning] = useState('');
  const requestSequence = useRef(0);
  const {
    enhanceTableColumns,
    queryFromTableChange,
    appendObjectTableQueryParams,
    formatNumber,
    formatAmount,
  } = tableTools;
  const label = kind === 'session' ? t('会话 ID') : t('缓存关联 ID');
  const userLabel = (user) => `${user.username || '-'} (#${user.user_id})`;
  const tokenLabel = (token) =>
    `${token.token_name || '-'} (#${token.token_id}, U${token.user_id})`;
  const labels = (items, render) => (
    <Space wrap>
      {(items || []).slice(0, 3).map((item, i) => (
        <Tag key={i}>{render(item)}</Tag>
      ))}
      {items?.length > 3 && <Tag>+{items.length - 3}</Tag>}
    </Space>
  );
  const load = async (number = 1, size = pageSize, sort = tableQuery) => {
    const sequence = ++requestSequence.current;
    setLoading(true);
    try {
      const params = {
        ...query,
        kind,
        p: number,
        page_size: size,
        sort: sort.sort,
        order: sort.order,
      };
      appendObjectTableQueryParams(params, sort);
      const data = await API.get('/api/enhancements/risk/shared-sessions', {
        params,
      }).then(unwrap);
      if (sequence === requestSequence.current) setPage(data);
    } catch (err) {
      if (sequence === requestSequence.current) {
        setPage(EMPTY);
        showError(err.message);
      }
    } finally {
      if (sequence === requestSequence.current) setLoading(false);
    }
  };
  useEffect(() => {
    setSelected(null);
    load(1);
    return () => {
      requestSequence.current++;
    };
  }, [query, refreshKey]);

  const viewLogs = (record, window) => {
    const params = new URLSearchParams({
      [kind === 'session' ? 'session_id' : 'cache_key']: record.identifier,
      start_timestamp: String(window.start),
      end_timestamp: String(window.end),
    });
    navigate(`/console/log?${params.toString()}`);
  };

  const ban = (record, window) => {
    let selectedUserIds = record.users.map((user) => user.user_id);
    let duration = 0;
    let reason =
      kind === 'session' ? t('共用会话 ID 风控封禁') : t('缓存关联风控封禁');
    Modal.confirm({
      title: t('封禁用户'),
      content: (
        <BanConfirmContent
          users={record.users}
          reason={reason}
          description={
            <div className='break-all'>
              {t('将封禁当前时间范围内与该标识关联的选中用户')}：
              {record.identifier}
            </div>
          }
          onSelectedUserIdsChange={(ids) => {
            selectedUserIds = ids;
          }}
          onDurationChange={(value) => {
            duration = value;
          }}
          onReasonChange={(value) => {
            reason = value;
          }}
        />
      ),
      okText: t('确认封禁'),
      cancelText: t('取消'),
      okButtonProps: { type: 'danger' },
      onOk: async () => {
        if (!selectedUserIds.length) {
          showError(t('请选择至少一个用户'));
          return false;
        }
        if (!isDisableDurationValid(duration)) {
          showError(t('禁用时长必须为有效范围内的非负整数'));
          return false;
        }
        setBanning(record.identifier);
        try {
          const result = await API.post(
            '/api/enhancements/risk/shared-sessions/ban-users',
            {
              kind,
              identifier: record.identifier,
              start: window.start,
              end: window.end,
              user_ids: selectedUserIds,
              duration_minutes: Number(duration),
              reason,
            },
          ).then(unwrap);
          if (result.success)
            showSuccess(
              `${t('已封禁用户')}：${result.success} / ${result.total_users}`,
            );
          if (result.failures?.length)
            Modal.error({
              title: t('部分用户封禁失败'),
              content: result.failures.map((failure) => (
                <div key={failure.id}>
                  #{failure.id}: {failure.message}
                </div>
              )),
            });
          await load(page.page);
        } catch (err) {
          showError(err.message);
          return false;
        } finally {
          setBanning('');
        }
      },
    });
  };

  const columns = [
    {
      title: label,
      dataIndex: 'identifier',
      copyable: false,
      fixed: 'left',
      width: 240,
      render: (value) => (
        <Typography.Text
          style={{ maxWidth: 220 }}
          ellipsis={{ showTooltip: true }}
          copyable
        >
          {value}
        </Typography.Text>
      ),
    },
    {
      title: t('标识来源'),
      dataIndex: 'sources',
      width: 240,
      render: (values) => labels(values, (value) => value),
    },
    ...[
      ['user_count', t('用户数')],
      ['token_count', t('令牌数')],
      ['request_count', t('请求数')],
      ['error_count', t('错误数')],
    ].map(([dataIndex, title]) => ({ title, dataIndex, render: formatNumber })),
    { title: t('金额'), dataIndex: 'quota', render: formatAmount },
    {
      title: t('首次出现'),
      dataIndex: 'first_seen_at',
      width: 180,
      render: timestamp2string,
    },
    {
      title: t('最后出现'),
      dataIndex: 'last_seen_at',
      width: 180,
      render: timestamp2string,
    },
    {
      title: t('用户'),
      dataIndex: 'users',
      width: 250,
      render: (values) => labels(values, userLabel),
    },
    {
      title: t('令牌'),
      dataIndex: 'tokens',
      width: 250,
      render: (values) => labels(values, tokenLabel),
    },
    {
      title: 'IP',
      dataIndex: 'ips',
      width: 220,
      render: (values) => labels(values, (value) => value),
    },
    {
      title: t('操作'),
      dataIndex: 'operate',
      fixed: 'right',
      width: 230,
      render: (_, record) => (
        <Space>
          <Button
            size='small'
            theme='borderless'
            onClick={() =>
              setSelected({ record, start: page.start, end: page.end })
            }
          >
            {t('查看')}
          </Button>
          <Button
            size='small'
            theme='borderless'
            type='danger'
            loading={banning === record.identifier}
            onClick={() => ban(record, page)}
          >
            {t('封禁用户')}
          </Button>
        </Space>
      ),
    },
  ];
  return (
    <>
      {kind === 'cache' && (
        <div className='mb-3 text-semi-color-text-2'>
          {t(
            '缓存关联是较弱线索，共用缓存配置也可能命中，请结合请求记录人工判断。',
          )}
        </div>
      )}
      <Table
        size='small'
        rowKey='identifier'
        columns={enhanceTableColumns(columns, { t, tableQuery }).map(
          (column) => ({
            ...column,
            onFilter: undefined,
            ...(['sources', 'users', 'tokens', 'ips'].includes(column.dataIndex)
              ? { sorter: false }
              : {}),
          }),
        )}
        dataSource={page.items}
        loading={loading}
        scroll={{ x: 'max-content' }}
        empty={<Empty description={t('暂无数据')} />}
        onChange={(change) => {
          const next = queryFromTableChange(change, tableQuery);
          // Pagination also fires Table.onChange; its own handler loads the
          // requested page. Only reset to page one for a changed sort/filter.
          if (
            next.sort === tableQuery.sort &&
            next.order === tableQuery.order &&
            JSON.stringify(next.filters) === JSON.stringify(tableQuery.filters)
          ) {
            return;
          }
          setTableQuery(next);
          load(1, pageSize, next);
        }}
        pagination={{
          currentPage: page.page,
          pageSize,
          total: page.total,
          showSizeChanger: true,
          pageSizeOptions: [10, 20, 50, 100],
          onPageChange: (number) => load(number),
          onPageSizeChange: (size) => {
            setPageSize(size);
            load(1, size);
          },
        }}
      />
      <SideSheet
        title={label}
        visible={Boolean(selected)}
        width={800}
        onCancel={() => setSelected(null)}
        footer={
          <Space>
            <Button onClick={() => viewLogs(selected.record, selected)}>
              {t('查看相关日志')}
            </Button>
            <Button
              type='danger'
              loading={banning === selected?.record.identifier}
              onClick={() => ban(selected.record, selected)}
            >
              {t('封禁用户')}
            </Button>
            <Button onClick={() => setSelected(null)}>{t('关闭')}</Button>
          </Space>
        }
      >
        {selected && (
          <div className='space-y-4'>
            <Typography.Text copyable className='break-all'>
              {selected.record.identifier}
            </Typography.Text>
            <div>
              {t('时间范围')}：{timestamp2string(selected.start)} —{' '}
              {timestamp2string(selected.end)}
            </div>
            <div>
              {t('标识来源')}：{selected.record.sources.join(', ')}
            </div>
            <Table
              size='small'
              rowKey='user_id'
              pagination={false}
              dataSource={selected.record.users}
              columns={[
                { title: t('用户 ID'), dataIndex: 'user_id' },
                { title: t('用户名'), dataIndex: 'username' },
                { title: t('请求数'), dataIndex: 'request_count' },
              ]}
            />
            <Table
              size='small'
              rowKey='token_id'
              pagination={false}
              dataSource={selected.record.tokens}
              columns={[
                { title: t('令牌 ID'), dataIndex: 'token_id' },
                { title: t('令牌名称'), dataIndex: 'token_name' },
                { title: t('用户'), render: (_, user) => userLabel(user) },
                { title: t('请求数'), dataIndex: 'request_count' },
              ]}
            />
            <div className='break-all'>
              {t('IP')}：{selected.record.ips.join(', ') || '-'}
            </div>
          </div>
        )}
      </SideSheet>
    </>
  );
}

export default function SessionRisk(props) {
  const { t } = useTranslation();
  return (
    <Card title={t('会话 ID 风控')} className='!rounded-lg'>
      <Tabs defaultActiveKey='session' keepDOM>
        <TabPane tab={t('共用会话 ID')} itemKey='session'>
          <SessionRiskTable {...props} kind={SESSION_KIND} />
        </TabPane>
        <TabPane tab={t('缓存关联')} itemKey='cache'>
          <SessionRiskTable {...props} kind={CACHE_KIND} />
        </TabPane>
      </Tabs>
    </Card>
  );
}
