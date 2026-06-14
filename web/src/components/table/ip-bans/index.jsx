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

import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import {
  Button,
  Col,
  Empty,
  Form,
  Input,
  Modal,
  Row,
  SideSheet,
  Space,
  Spin,
  Tag,
  Tooltip,
  Typography,
} from '@douyinfe/semi-ui';
import {
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';
import {
  Info,
  Pencil,
  Plus,
  RefreshCw,
  Save,
  Search,
  Trash2,
  Upload,
  X,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import CardPro from '../../common/ui/CardPro';
import CardTable from '../../common/ui/CardTable';
import {
  API,
  showError,
  showSuccess,
  timestamp2string,
} from '../../../helpers';
import { ITEMS_PER_PAGE } from '../../../constants';
import { createCardProPagination } from '../../../helpers/utils';
import { useIsMobile } from '../../../hooks/common/useIsMobile';

const { Text, Title } = Typography;

const IP_BAN_TYPES = {
  PERMANENT: 'permanent',
  TEMPORARY: 'temporary',
};

const iconProps = { size: 16, strokeWidth: 2 };

const getNow = () => Math.floor(Date.now() / 1000);

const formatExpiresAt = (expiresAt, t) => {
  if (!expiresAt) {
    return t('永久');
  }
  return timestamp2string(expiresAt);
};

const renderStatus = (record, t) => {
  if (!record.expires_at) {
    return (
      <Tag color='red' shape='circle'>
        {t('永久封禁')}
      </Tag>
    );
  }
  if (record.expires_at <= getNow()) {
    return (
      <Tag color='grey' shape='circle'>
        {t('已过期')}
      </Tag>
    );
  }
  return (
    <Tag color='orange' shape='circle'>
      {t('临时封禁')}
    </Tag>
  );
};

const renderTargetLabel = (t, labelKey = 'IP或IP段') => (
  <span className='inline-flex items-center gap-1'>
    {t(labelKey)}
    <Tooltip
      content={t(
        'IPv6 CIDR 按位匹配：2602::/64 只匹配 2602:0:0:0:*；要封禁 2602:feda:* 请使用 2602:feda::/32。',
      )}
    >
      <Info size={14} strokeWidth={2} className='text-gray-400 cursor-help' />
    </Tooltip>
  </span>
);

const useIPBanList = (type) => {
  const { t } = useTranslation();
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(false);
  const [keyword, setKeyword] = useState('');
  const [activePage, setActivePage] = useState(1);
  const [pageSize, setPageSize] = useState(ITEMS_PER_PAGE);
  const [total, setTotal] = useState(0);

  const load = useCallback(
    async (page = activePage, size = pageSize, search = keyword) => {
      setLoading(true);
      try {
        const url =
          search.trim() === '' ? '/api/ip_ban/' : '/api/ip_ban/search';
        const res = await API.get(url, {
          params: {
            type,
            keyword: search.trim() || undefined,
            p: page,
            page_size: size,
          },
        });
        const { success, message, data } = res.data;
        if (success) {
          setItems(data.items || []);
          setActivePage(data.page <= 0 ? 1 : data.page);
          setTotal(data.total || 0);
        } else {
          showError(message);
        }
      } catch (error) {
        showError(error.message);
      } finally {
        setLoading(false);
      }
    },
    [activePage, keyword, pageSize, type],
  );

  useEffect(() => {
    load(1, pageSize, '');
  }, [type]);

  const search = () => {
    setActivePage(1);
    load(1, pageSize, keyword);
  };

  const refresh = () => load(activePage, pageSize, keyword);

  const handlePageChange = (page) => {
    setActivePage(page);
    load(page, pageSize, keyword);
  };

  const handlePageSizeChange = (size) => {
    setPageSize(size);
    setActivePage(1);
    load(1, size, keyword);
  };

  return {
    t,
    items,
    loading,
    keyword,
    setKeyword,
    activePage,
    pageSize,
    total,
    load,
    search,
    refresh,
    handlePageChange,
    handlePageSizeChange,
  };
};

const handleSelfLockResponse = (res, retry, t) => {
  const { success, message, data } = res.data;
  if (success) {
    return false;
  }
  if (data?.requires_confirmation) {
    Modal.confirm({
      title: t('确认封禁当前IP'),
      content: `${t('该规则会封禁你当前访问后台使用的IP')}：${data.client_ip}`,
      onOk: retry,
    });
    return true;
  }
  showError(message);
  return true;
};

const EditIPBanModal = ({ visible, type, record, onClose, onSaved }) => {
  const { t } = useTranslation();
  const isEdit = Boolean(record?.id);
  const [loading, setLoading] = useState(false);
  const formApiRef = useRef(null);
  const isMobile = useIsMobile();

  const initValues = useMemo(
    () => ({
      target: record?.target || '',
      reason: record?.reason || '',
      expires_at: record?.expires_at
        ? new Date(record.expires_at * 1000)
        : null,
    }),
    [record],
  );

  useEffect(() => {
    if (visible) {
      formApiRef.current?.setValues(initValues);
    }
  }, [initValues, visible]);

  const submit = async (values, confirmSelfLock = false) => {
    const expiresAt =
      type === IP_BAN_TYPES.TEMPORARY
        ? values.expires_at
          ? Math.floor(values.expires_at.getTime() / 1000)
          : 0
        : 0;
    if (type === IP_BAN_TYPES.TEMPORARY && !expiresAt) {
      showError(t('请选择临时封禁过期时间'));
      return;
    }
    setLoading(true);
    try {
      const payload = {
        id: record?.id,
        target: values.target,
        reason: values.reason,
        expires_at: expiresAt,
        confirm_self_lock: confirmSelfLock,
      };
      const res = isEdit
        ? await API.put('/api/ip_ban/', payload)
        : await API.post('/api/ip_ban/', payload);

      if (handleSelfLockResponse(res, () => submit(values, true), t)) {
        return;
      }
      showSuccess(isEdit ? t('更新成功') : t('添加成功'));
      onSaved();
      onClose();
    } catch (error) {
      showError(error.message);
    } finally {
      setLoading(false);
    }
  };

  return (
    <SideSheet
      title={
        <Title heading={4} className='m-0'>
          {isEdit ? t('编辑IP封禁') : t('添加IP封禁')}
        </Title>
      }
      visible={visible}
      width={isMobile ? '100%' : 560}
      bodyStyle={{ padding: 0 }}
      closeIcon={null}
      onCancel={onClose}
      footer={
        <div className='flex justify-end'>
          <Space>
            <Button
              type='primary'
              icon={<Save {...iconProps} />}
              loading={loading}
              onClick={() => formApiRef.current?.submitForm()}
            >
              {t('保存')}
            </Button>
            <Button
              type='tertiary'
              icon={<X {...iconProps} />}
              onClick={onClose}
            >
              {t('取消')}
            </Button>
          </Space>
        </div>
      }
    >
      <Spin spinning={loading}>
        <Form
          initValues={initValues}
          getFormApi={(api) => (formApiRef.current = api)}
          onSubmit={submit}
        >
          <div className='p-4'>
            <Form.Input
              field='target'
              label={renderTargetLabel(t)}
              placeholder={t('IP或IP段示例')}
              rules={[{ required: true, message: t('请输入IP或IP段') }]}
              showClear
            />
            <Form.TextArea
              field='reason'
              label={t('封禁原因')}
              placeholder={t('请输入封禁原因')}
              rows={3}
              autosize
              rules={[{ required: true, message: t('请输入封禁原因') }]}
              showClear
            />
            {type === IP_BAN_TYPES.TEMPORARY && (
              <Form.DatePicker
                field='expires_at'
                label={t('过期时间')}
                type='dateTime'
                placeholder={t('请选择临时封禁过期时间')}
                rules={[
                  { required: true, message: t('请选择临时封禁过期时间') },
                ]}
                style={{ width: '100%' }}
              />
            )}
          </div>
        </Form>
      </Spin>
    </SideSheet>
  );
};

const BatchImportModal = ({ visible, type, onClose, onSaved }) => {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState(null);
  const formApiRef = useRef(null);

  useEffect(() => {
    if (visible) {
      setResult(null);
      formApiRef.current?.setValues({
        lines: '',
        default_reason: '',
        expires_at: null,
      });
    }
  }, [visible]);

  const submit = async (values, confirmSelfLock = false) => {
    const expiresAt =
      type === IP_BAN_TYPES.TEMPORARY
        ? values.expires_at
          ? Math.floor(values.expires_at.getTime() / 1000)
          : 0
        : 0;
    if (type === IP_BAN_TYPES.TEMPORARY && !expiresAt) {
      showError(t('请选择临时封禁过期时间'));
      return;
    }
    setLoading(true);
    try {
      const res = await API.post('/api/ip_ban/batch', {
        lines: values.lines,
        default_reason: values.default_reason,
        expires_at: expiresAt,
        confirm_self_lock: confirmSelfLock,
      });
      if (handleSelfLockResponse(res, () => submit(values, true), t)) {
        return;
      }
      const data = res.data.data || {};
      setResult(data);
      showSuccess(
        `${t('导入完成')}：${t('成功')} ${data.created || 0}，${t('跳过')} ${
          data.skipped || 0
        }，${t('无效')} ${data.invalid?.length || 0}`,
      );
      onSaved();
      if (!data.invalid || data.invalid.length === 0) {
        onClose();
      }
    } catch (error) {
      showError(error.message);
    } finally {
      setLoading(false);
    }
  };

  return (
    <Modal
      title={t('批量导入IP封禁')}
      visible={visible}
      width={720}
      onCancel={onClose}
      footer={
        <Space>
          <Button
            type='primary'
            icon={<Upload {...iconProps} />}
            loading={loading}
            onClick={() => formApiRef.current?.submitForm()}
          >
            {t('导入')}
          </Button>
          <Button type='tertiary' onClick={onClose}>
            {t('关闭')}
          </Button>
        </Space>
      }
    >
      <Form getFormApi={(api) => (formApiRef.current = api)} onSubmit={submit}>
        <Form.TextArea
          field='lines'
          label={renderTargetLabel(t, 'IP或IP段列表')}
          placeholder={`203.0.113.10\n203.0.113.0/24 ${t('封禁原因')}\n2602:feda::/32 ${t('封禁原因')}`}
          rows={8}
          autosize
          rules={[{ required: true, message: t('请输入IP或IP段') }]}
        />
        <Form.TextArea
          field='default_reason'
          label={t('统一封禁原因')}
          placeholder={t('未填写行内原因时使用该原因')}
          rows={2}
          autosize
          showClear
        />
        {type === IP_BAN_TYPES.TEMPORARY && (
          <Form.DatePicker
            field='expires_at'
            label={t('过期时间')}
            type='dateTime'
            placeholder={t('请选择临时封禁过期时间')}
            rules={[{ required: true, message: t('请选择临时封禁过期时间') }]}
            style={{ width: '100%' }}
          />
        )}
      </Form>
      {result?.invalid?.length > 0 && (
        <div className='mt-3'>
          <Text strong>{t('无效行')}</Text>
          <div className='mt-2 max-h-40 overflow-auto text-xs'>
            {result.invalid.map((item) => (
              <div key={item.line_number} className='py-1 break-all'>
                {t('第')} {item.line_number} {t('行')}：{item.message}
              </div>
            ))}
          </div>
        </div>
      )}
    </Modal>
  );
};

const IPBanSection = ({ type, title, description }) => {
  const list = useIPBanList(type);
  const isMobile = useIsMobile();
  const [editingRecord, setEditingRecord] = useState(null);
  const [showEdit, setShowEdit] = useState(false);
  const [showBatch, setShowBatch] = useState(false);
  const { t } = list;

  const deleteRecord = (record) => {
    Modal.confirm({
      title: t('确认删除'),
      content: `${t('删除后该IP封禁规则将立即失效')}：${record.target}`,
      onOk: async () => {
        try {
          const res = await API.delete(`/api/ip_ban/${record.id}`);
          const { success, message } = res.data;
          if (success) {
            showSuccess(t('删除成功'));
            list.refresh();
          } else {
            showError(message);
          }
        } catch (error) {
          showError(error.message);
        }
      },
    });
  };

  const columns = useMemo(
    () => [
      {
        title: t('IP或IP段'),
        dataIndex: 'target',
        render: (text) => (
          <Tag color='white' shape='circle'>
            {text}
          </Tag>
        ),
      },
      {
        title: t('状态'),
        dataIndex: 'expires_at',
        render: (_text, record) => renderStatus(record, t),
      },
      {
        title: t('原因'),
        dataIndex: 'reason',
        render: (text) => <span className='break-all'>{text}</span>,
      },
      {
        title: t('过期时间'),
        dataIndex: 'expires_at',
        render: (text) => formatExpiresAt(text, t),
      },
      {
        title: '',
        dataIndex: 'operate',
        fixed: 'right',
        width: 150,
        render: (_text, record) => (
          <Space>
            <Button
              size='small'
              type='tertiary'
              icon={<Pencil {...iconProps} />}
              onClick={() => {
                setEditingRecord(record);
                setShowEdit(true);
              }}
            />
            <Button
              size='small'
              type='danger'
              icon={<Trash2 {...iconProps} />}
              onClick={() => deleteRecord(record)}
            />
          </Space>
        ),
      },
    ],
    [t],
  );

  return (
    <>
      <EditIPBanModal
        visible={showEdit}
        type={type}
        record={editingRecord}
        onClose={() => setShowEdit(false)}
        onSaved={list.refresh}
      />
      <BatchImportModal
        visible={showBatch}
        type={type}
        onClose={() => setShowBatch(false)}
        onSaved={list.refresh}
      />
      <CardPro
        type='type1'
        descriptionArea={
          <div>
            <Title heading={5} className='m-0'>
              {title}
            </Title>
            <Text type='secondary' size='small'>
              {description}
            </Text>
          </div>
        }
        actionsArea={
          <div className='flex flex-col md:flex-row gap-2 justify-between'>
            <div className='flex flex-wrap gap-2'>
              <Button
                type='primary'
                size='small'
                icon={<Plus {...iconProps} />}
                onClick={() => {
                  setEditingRecord(null);
                  setShowEdit(true);
                }}
              >
                {t('添加')}
              </Button>
              <Button
                type='tertiary'
                size='small'
                icon={<Upload {...iconProps} />}
                onClick={() => setShowBatch(true)}
              >
                {t('批量导入')}
              </Button>
              <Button
                type='tertiary'
                size='small'
                icon={<RefreshCw {...iconProps} />}
                onClick={list.refresh}
              />
            </div>
            <div className='flex gap-2'>
              <Input
                value={list.keyword}
                onChange={list.setKeyword}
                onEnterPress={list.search}
                prefix={<Search {...iconProps} />}
                placeholder={t('搜索IP或原因')}
                showClear
              />
              <Button
                type='tertiary'
                size='small'
                icon={<Search {...iconProps} />}
                onClick={list.search}
              />
            </div>
          </div>
        }
        paginationArea={createCardProPagination({
          currentPage: list.activePage,
          pageSize: list.pageSize,
          total: list.total,
          onPageChange: list.handlePageChange,
          onPageSizeChange: list.handlePageSizeChange,
          isMobile,
          t,
        })}
        t={t}
      >
        <CardTable
          rowKey='id'
          columns={columns}
          dataSource={list.items}
          loading={list.loading}
          hidePagination
          scroll={{ x: 'max-content' }}
          onRow={(record) =>
            record.expires_at && record.expires_at <= getNow()
              ? { style: { background: 'var(--semi-color-disabled-border)' } }
              : {}
          }
          empty={
            <Empty
              image={
                <IllustrationNoResult style={{ width: 150, height: 150 }} />
              }
              darkModeImage={
                <IllustrationNoResultDark style={{ width: 150, height: 150 }} />
              }
              description={t('暂无数据')}
              style={{ padding: 30 }}
            />
          }
          className='rounded-xl overflow-hidden'
          size='middle'
        />
      </CardPro>
    </>
  );
};

const IPBansPage = () => {
  const { t } = useTranslation();

  return (
    <Row gutter={[12, 12]}>
      <Col xs={24} sm={24} md={12} lg={12} xl={12}>
        <IPBanSection
          type={IP_BAN_TYPES.PERMANENT}
          title={t('永久封禁')}
          description={t('长期阻止指定IP或IP段访问全站')}
        />
      </Col>
      <Col xs={24} sm={24} md={12} lg={12} xl={12}>
        <IPBanSection
          type={IP_BAN_TYPES.TEMPORARY}
          title={t('临时封禁')}
          description={t('在指定时间前阻止指定IP或IP段访问全站')}
        />
      </Col>
    </Row>
  );
};

export default IPBansPage;
