import React, { useEffect, useMemo, useState } from 'react';
import {
  Button,
  Card,
  Col,
  Form,
  InputNumber,
  Modal,
  Row,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
  Progress,
} from '@douyinfe/semi-ui';
import { IconDownload, IconDelete, IconSearch, IconSave } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess, timestamp2string } from '../../../helpers';

const { Text, Title } = Typography;

function formatBytes(bytes, decimals = 2) {
  if (bytes === null || bytes === undefined || Number.isNaN(Number(bytes))) return '0 Bytes';
  const value = Number(bytes);
  if (value === 0) return '0 Bytes';
  const k = 1024;
  const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(Math.floor(Math.log(Math.abs(value)) / Math.log(k)), sizes.length - 1);
  return `${parseFloat((value / Math.pow(k, i)).toFixed(decimals))} ${sizes[i]}`;
}

function toTimestamp(value) {
  if (!value) return 0;
  if (value instanceof Date) return Math.floor(value.getTime() / 1000);
  const parsed = Date.parse(value);
  return Number.isNaN(parsed) ? 0 : Math.floor(parsed / 1000);
}

function downloadBlob(blob, filename) {
  const url = window.URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  window.URL.revokeObjectURL(url);
}

export default function SettingsConversationLogs() {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [summaryLoading, setSummaryLoading] = useState(false);
  const [actionLoading, setActionLoading] = useState(false);
  const [summary, setSummary] = useState(null);
  const [settings, setSettings] = useState({ retention_days: 30, max_storage_gb: 50 });
  const [logs, setLogs] = useState([]);
  const [pagination, setPagination] = useState({ currentPage: 1, pageSize: 10, total: 0 });
  const [formApi, setFormApi] = useState(null);
  const [detail, setDetail] = useState(null);
  const [exportProgress, setExportProgress] = useState({
    visible: false,
    title: '',
    phase: 'idle',
    percent: 0,
    recordCount: 0,
    estimateBytes: 0,
    loadedBytes: 0,
    message: '',
  });

  const getFilterParams = () => {
    const values = formApi ? formApi.getValues() : {};
    const params = {
      username: values.username || '',
      token_name: values.token_name || '',
      model_name: values.model_name || '',
      channel_id: values.channel_id || '',
      group: values.group || '',
      request_id: values.request_id || '',
      exported: values.exported || '',
    };
    if (values.dateRange && Array.isArray(values.dateRange) && values.dateRange.length === 2) {
      params.start_timestamp = toTimestamp(values.dateRange[0]);
      params.end_timestamp = toTimestamp(values.dateRange[1]);
    }
    return params;
  };

  const fetchSummary = async () => {
    try {
      setSummaryLoading(true);
      const res = await API.get('/api/conversation_logs/summary');
      const { success, message, data } = res.data;
      if (!success) {
        showError(message);
        return;
      }
      setSummary(data.summary);
      setSettings(data.settings || { retention_days: 30, max_storage_gb: 50 });
    } finally {
      setSummaryLoading(false);
    }
  };

  const fetchLogs = async (page = pagination.currentPage, pageSize = pagination.pageSize) => {
    try {
      setLoading(true);
      const res = await API.get('/api/conversation_logs/', {
        params: { ...getFilterParams(), p: page, page_size: pageSize },
      });
      const { success, message, data } = res.data;
      if (!success) {
        showError(message);
        return;
      }
      setLogs(data.items || []);
      setPagination({ currentPage: data.page, pageSize: data.page_size, total: data.total });
    } finally {
      setLoading(false);
    }
  };

  const saveSettings = async () => {
    setActionLoading(true);
    try {
      const res = await API.put('/api/conversation_logs/settings', {
        retention_days: Number(settings.retention_days || 0),
        max_storage_gb: Number(settings.max_storage_gb || 0),
      });
      const { success, message } = res.data;
      if (!success) {
        showError(message);
        return;
      }
      showSuccess(t('保存成功'));
      fetchSummary();
    } finally {
      setActionLoading(false);
    }
  };

  const updateExportProgress = (patch) => {
    setExportProgress((prev) => ({ ...prev, ...patch }));
  };

  const fetchExportSummary = async (params) => {
    updateExportProgress({
      visible: true,
      phase: 'preparing',
      percent: 3,
      loadedBytes: 0,
      message: t('正在统计当前筛选结果'),
    });
    const res = await API.get('/api/conversation_logs/export_summary', {
      params,
      disableDuplicate: true,
    });
    const { success, message, data } = res.data;
    if (!success) {
      throw new Error(message || t('导出统计失败'));
    }
    updateExportProgress({
      phase: 'packaging',
      percent: data.record_count > 0 ? 5 : 100,
      recordCount: data.record_count || 0,
      estimateBytes: data.storage_bytes || 0,
      message:
        data.record_count > 0
          ? t('服务器正在流式打包 ZIP')
          : t('当前筛选结果没有记录'),
    });
    return data;
  };

  const runExportWithProgress = async ({ deleteAfterExport = false } = {}) => {
    const params = getFilterParams();
    setActionLoading(true);
    try {
      updateExportProgress({
        visible: true,
        title: deleteAfterExport ? t('导出并删除进度') : t('导出进度'),
        phase: 'preparing',
        percent: 0,
        recordCount: 0,
        estimateBytes: 0,
        loadedBytes: 0,
        message: t('准备导出'),
      });
      const exportSummary = await fetchExportSummary(params);
      const estimateBytes = Number(exportSummary.storage_bytes || 0);
      const requestConfig = {
        responseType: 'blob',
        skipErrorHandler: true,
        disableDuplicate: true,
        onDownloadProgress: (event) => {
          const loadedBytes = Number(event.loaded || 0);
          const totalBytes = Number(event.total || 0);
          const denominator = totalBytes > 0 ? totalBytes : estimateBytes;
          const percent =
            denominator > 0
              ? Math.min(98, Math.max(5, Math.round((loadedBytes / denominator) * 100)))
              : 15;
          updateExportProgress({
            phase: 'downloading',
            percent,
            loadedBytes,
            message: t('正在接收 ZIP 数据'),
          });
        },
      };
      const res = deleteAfterExport
        ? await API.post('/api/conversation_logs/export_and_delete', params, requestConfig)
        : await API.get('/api/conversation_logs/export.zip', {
            ...requestConfig,
            params,
          });
      updateExportProgress({
        phase: 'saving',
        percent: 99,
        message: t('正在保存文件'),
      });
      downloadBlob(res.data, `conversation-logs-${Date.now()}.zip`);
      updateExportProgress({
        phase: 'done',
        percent: 100,
        loadedBytes: res.data?.size || estimateBytes,
        message: deleteAfterExport ? t('导出并删除完成') : t('导出完成'),
      });
      showSuccess(deleteAfterExport ? t('导出并删除完成') : t('导出成功'));
      fetchSummary();
      if (deleteAfterExport) {
        fetchLogs(1, pagination.pageSize);
      }
    } catch (error) {
      updateExportProgress({
        phase: 'error',
        message: error?.message || t('导出失败'),
      });
      showError(error?.message || t('导出失败'));
    } finally {
      setActionLoading(false);
    }
  };

  const exportZip = async () => {
    await runExportWithProgress();
  };

  const exportAndDelete = () => {
    Modal.confirm({
      title: t('导出并删除'),
      content: t('将按当前筛选条件导出 ZIP，下载完成后删除命中的记录。'),
      okText: t('确认'),
      cancelText: t('取消'),
      onOk: () => runExportWithProgress({ deleteAfterExport: true }),
    });
  };

  const deleteFiltered = () => {
    Modal.confirm({
      title: t('直接删除'),
      content: t('将直接删除当前筛选条件命中的完整对话记录。'),
      okText: t('删除'),
      cancelText: t('取消'),
      onOk: async () => {
        setActionLoading(true);
        try {
          const res = await API.delete('/api/conversation_logs/', { params: getFilterParams() });
          const { success, message, data } = res.data;
          if (!success) {
            showError(message);
            return;
          }
          showSuccess(t('已删除 {{count}} 条记录', { count: data.deleted || 0 }));
          fetchSummary();
          fetchLogs(1, pagination.pageSize);
        } finally {
          setActionLoading(false);
        }
      },
    });
  };

  const openDetail = async (record) => {
    const res = await API.get(`/api/conversation_logs/${record.id}`);
    const { success, message, data } = res.data;
    if (!success) {
      showError(message);
      return;
    }
    setDetail(data);
  };

  useEffect(() => {
    fetchSummary();
    fetchLogs(1, pagination.pageSize);
  }, [formApi]);

  const columns = useMemo(
    () => [
      { title: t('时间'), dataIndex: 'created_at', render: (v) => (v ? timestamp2string(v) : '-') },
      { title: t('Request ID'), dataIndex: 'request_id', render: (v) => <Text copyable={{ content: v }}>{v || '-'}</Text> },
      { title: t('用户'), dataIndex: 'username', render: (v, r) => v || r.user_id || '-' },
      { title: t('模型'), dataIndex: 'model_name' },
      { title: t('渠道'), dataIndex: 'channel_id' },
      { title: t('分组'), dataIndex: 'group' },
      { title: t('大小'), dataIndex: 'storage_bytes', render: (v) => formatBytes(v) },
      {
        title: t('状态'),
        dataIndex: 'exported_at',
        render: (v) => (v ? <Tag color='green'>{t('已导出')}</Tag> : <Tag>{t('未导出')}</Tag>),
      },
      {
        title: t('操作'),
        render: (_, record) => (
          <Button size='small' onClick={() => openDetail(record)}>
            {t('查看')}
          </Button>
        ),
      },
    ],
    [t],
  );

  const usagePercent =
    summary && settings?.max_storage_bytes > 0
      ? Math.min(100, (summary.storage_bytes / settings.max_storage_bytes) * 100)
      : 0;

  return (
    <Spin spinning={summaryLoading || loading}>
      <div className='space-y-3'>
        <Card>
          <div className='flex flex-col gap-3'>
            <div className='flex items-center justify-between gap-2 flex-wrap'>
              <Title heading={5}>{t('完整对话采集')}</Title>
              <Space>
                <Button icon={<IconSave />} loading={actionLoading} onClick={saveSettings}>
                  {t('保存配置')}
                </Button>
              </Space>
            </div>
            <Row gutter={16}>
              <Col xs={24} md={6}>
                <Text type='tertiary'>{t('当前占用')}</Text>
                <div className='text-lg font-medium'>{formatBytes(summary?.storage_bytes || 0)}</div>
              </Col>
              <Col xs={24} md={6}>
                <Text type='tertiary'>{t('最大上限')}</Text>
                <InputNumber
                  min={0}
                  value={settings.max_storage_gb}
                  suffix='GB'
                  onChange={(value) => setSettings((prev) => ({ ...prev, max_storage_gb: value }))}
                />
              </Col>
              <Col xs={24} md={6}>
                <Text type='tertiary'>{t('保留天数')}</Text>
                <InputNumber
                  min={0}
                  value={settings.retention_days}
                  suffix={t('天')}
                  onChange={(value) => setSettings((prev) => ({ ...prev, retention_days: value }))}
                />
              </Col>
              <Col xs={24} md={6}>
                <Text type='tertiary'>{t('记录数')}</Text>
                <div className='text-lg font-medium'>
                  {summary?.record_count || 0} / {summary?.exported_count || 0} {t('已导出')}
                </div>
                <Text type='tertiary'>{usagePercent.toFixed(1)}%</Text>
              </Col>
            </Row>
            <Text type='tertiary'>
              {t('最早')}：{summary?.earliest_created_at ? timestamp2string(summary.earliest_created_at) : '-'} · {t('最新')}：
              {summary?.latest_created_at ? timestamp2string(summary.latest_created_at) : '-'}
            </Text>
          </div>
        </Card>

        <Card>
          <Form
            getFormApi={setFormApi}
            onSubmit={() => fetchLogs(1, pagination.pageSize)}
            allowEmpty
            layout='vertical'
          >
            <Row gutter={12}>
              <Col xs={24} md={8}>
                <Form.DatePicker
                  field='dateRange'
                  type='dateTimeRange'
                  label={t('时间范围')}
                  showClear
                  inputReadOnly
                />
              </Col>
              <Col xs={12} md={4}>
                <Form.Input field='username' label={t('用户')} showClear />
              </Col>
              <Col xs={12} md={4}>
                <Form.Input field='model_name' label={t('模型')} showClear />
              </Col>
              <Col xs={12} md={4}>
                <Form.Input field='channel_id' label={t('渠道 ID')} showClear />
              </Col>
              <Col xs={12} md={4}>
                <Form.Input field='group' label={t('分组')} showClear />
              </Col>
              <Col xs={12} md={6}>
                <Form.Input field='token_name' label={t('令牌')} showClear />
              </Col>
              <Col xs={12} md={6}>
                <Form.Input field='request_id' label={t('Request ID')} showClear />
              </Col>
              <Col xs={12} md={4}>
                <Form.Select field='exported' label={t('导出状态')} showClear>
                  <Form.Select.Option value='false'>{t('未导出')}</Form.Select.Option>
                  <Form.Select.Option value='true'>{t('已导出')}</Form.Select.Option>
                </Form.Select>
              </Col>
              <Col xs={24} md={8}>
                <Space style={{ marginTop: 30, flexWrap: 'wrap' }}>
                  <Button type='primary' icon={<IconSearch />} htmlType='submit'>
                    {t('筛选')}
                  </Button>
                  <Button icon={<IconDownload />} loading={actionLoading} onClick={exportZip}>
                    {t('下载 ZIP')}
                  </Button>
                  <Button type='warning' loading={actionLoading} onClick={exportAndDelete}>
                    {t('导出并删除')}
                  </Button>
                  <Button type='danger' icon={<IconDelete />} loading={actionLoading} onClick={deleteFiltered}>
                    {t('直接删除')}
                  </Button>
                </Space>
              </Col>
            </Row>
          </Form>
        </Card>

        <Card>
          <Table
            rowKey='id'
            columns={columns}
            dataSource={logs}
            pagination={{
              currentPage: pagination.currentPage,
              pageSize: pagination.pageSize,
              total: pagination.total,
              showSizeChanger: true,
              onPageChange: (page) => fetchLogs(page, pagination.pageSize),
              onPageSizeChange: (pageSize) => fetchLogs(1, pageSize),
            }}
          />
        </Card>

        <Modal
          title={t('对话详情')}
          visible={!!detail}
          onCancel={() => setDetail(null)}
          footer={null}
          width={900}
        >
          {detail && (
            <div className='space-y-3'>
              <Text copyable={{ content: detail.request_id }}>{detail.request_id}</Text>
              {[
                ['client_request_body', t('客户端请求')],
                ['upstream_request_body', t('上游请求')],
                ['upstream_response_body', t('上游响应')],
                ['client_response_body', t('客户端响应')],
                ['derived_assistant_text', t('Assistant 文本')],
                ['derived_tool_calls', t('工具调用')],
              ].map(([key, label]) => (
                <div key={key}>
                  <Text strong>{label}</Text>
                  <pre className='mt-1 p-2 bg-gray-50 rounded text-xs overflow-auto max-h-60 whitespace-pre-wrap'>
                    {detail[key] || ''}
                  </pre>
                </div>
              ))}
            </div>
          )}
        </Modal>

        <Modal
          title={exportProgress.title || t('导出进度')}
          visible={exportProgress.visible}
          maskClosable={false}
          closable={exportProgress.phase === 'done' || exportProgress.phase === 'error'}
          onCancel={() => updateExportProgress({ visible: false })}
          footer={
            exportProgress.phase === 'done' || exportProgress.phase === 'error' ? (
              <Button onClick={() => updateExportProgress({ visible: false })}>
                {t('关闭')}
              </Button>
            ) : null
          }
        >
          <div className='space-y-3'>
            <Progress
              percent={exportProgress.percent}
              status={exportProgress.phase === 'error' ? 'exception' : undefined}
              showInfo
            />
            <div className='grid grid-cols-1 md:grid-cols-3 gap-2 text-sm'>
              <div>
                <Text type='tertiary'>{t('记录数')}</Text>
                <div>{exportProgress.recordCount || 0}</div>
              </div>
              <div>
                <Text type='tertiary'>{t('估算原始大小')}</Text>
                <div>{formatBytes(exportProgress.estimateBytes || 0)}</div>
              </div>
              <div>
                <Text type='tertiary'>{t('已接收')}</Text>
                <div>{formatBytes(exportProgress.loadedBytes || 0)}</div>
              </div>
            </div>
            <Text type={exportProgress.phase === 'error' ? 'danger' : 'tertiary'}>
              {exportProgress.message}
            </Text>
          </div>
        </Modal>
      </div>
    </Spin>
  );
}
