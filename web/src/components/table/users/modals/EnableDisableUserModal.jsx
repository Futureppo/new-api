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
import {
  Banner,
  Button,
  Modal,
  Space,
  Spin,
  Table,
  Tag,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { API } from '../../../../helpers';

const { Text, Paragraph } = Typography;

const MAX_DISABLE_REASON_LENGTH = 255;

const getRelationTypeConfig = (relationType, t) => {
  if (relationType === 'target') {
    return { label: t('当前用户'), color: 'blue' };
  }
  if (relationType === 'inviter') {
    return { label: t('邀请人'), color: 'orange' };
  }
  return { label: t('被邀请用户'), color: 'green' };
};

const getUnavailableReasonLabel = (unavailableReason, t) => {
  switch (unavailableReason) {
    case 'deleted':
      return t('账号已注销');
    case 'already_disabled':
      return t('账号已禁用');
    case 'root_protected':
      return t('超级管理员不可禁用');
    case 'operator_self':
      return t('不能禁用当前管理员账号');
    case 'insufficient_permission':
      return t('权限不足');
    default:
      return t('账号状态不可操作');
  }
};

const EnableDisableUserModal = ({
  visible,
  onCancel,
  onConfirm,
  user,
  action,
  t,
}) => {
  const isDisable = action === 'disable';
  const [step, setStep] = useState('details');
  const [reason, setReason] = useState('');
  const [relations, setRelations] = useState(null);
  const [relationsLoading, setRelationsLoading] = useState(false);
  const [relationsError, setRelationsError] = useState('');
  const [reloadKey, setReloadKey] = useState(0);
  const [selectedRowKeys, setSelectedRowKeys] = useState([]);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (visible) {
      setStep(isDisable ? 'details' : 'confirm');
      setReason('');
      setRelations(null);
      setRelationsError('');
      setSelectedRowKeys([]);
      setSubmitting(false);
    }
  }, [visible, isDisable, user?.id]);

  useEffect(() => {
    if (!visible || !isDisable || !user?.id) {
      return undefined;
    }

    let cancelled = false;
    const loadRelations = async () => {
      setRelationsLoading(true);
      setRelationsError('');
      try {
        const res = await API.get(`/api/user/${user.id}/invite-relations`);
        if (cancelled) {
          return;
        }
        const { success, message, data } = res.data;
        if (!success) {
          setRelations(null);
          setRelationsError(message || t('加载关联账号失败'));
          return;
        }
        setRelations(data);
        const defaultSelectedIds = [
          data?.target,
          data?.inviter,
          ...(data?.invitees || []),
        ]
          .filter((item) => item?.selectable)
          .map((item) => item.id);
        setSelectedRowKeys(defaultSelectedIds);
      } catch (error) {
        if (!cancelled) {
          setRelations(null);
          setRelationsError(
            error?.response?.data?.message ||
              error.message ||
              t('加载关联账号失败'),
          );
        }
      } finally {
        if (!cancelled) {
          setRelationsLoading(false);
        }
      }
    };

    loadRelations();
    return () => {
      cancelled = true;
    };
  }, [visible, isDisable, user?.id, reloadKey, t]);

  const trimmedReason = reason.trim();

  const relationRows = useMemo(() => {
    if (!relations) {
      return [];
    }
    const candidates = [
      relations.target
        ? { ...relations.target, relation_type: 'target' }
        : null,
      relations.inviter
        ? { ...relations.inviter, relation_type: 'inviter' }
        : null,
      ...(relations.invitees || []).map((item) => ({
        ...item,
        relation_type: 'invitee',
      })),
    ].filter(Boolean);
    const seenIds = new Set();
    return candidates.filter((item) => {
      if (seenIds.has(item.id)) {
        return false;
      }
      seenIds.add(item.id);
      return true;
    });
  }, [relations]);

  const selectedIdSet = useMemo(
    () => new Set(selectedRowKeys.map((id) => Number(id))),
    [selectedRowKeys],
  );

  const selectedRows = useMemo(
    () => relationRows.filter((item) => selectedIdSet.has(item.id)),
    [relationRows, selectedIdSet],
  );

  const selectedCounts = useMemo(() => {
    return selectedRows.reduce(
      (counts, item) => {
        counts[item.relation_type] += 1;
        return counts;
      },
      { target: 0, inviter: 0, invitee: 0 },
    );
  }, [selectedRows]);

  const targetSelectable = Boolean(relations?.target?.selectable);
  const canContinue =
    Boolean(trimmedReason) &&
    Boolean(relations) &&
    !relationsLoading &&
    !relationsError &&
    targetSelectable &&
    selectedIdSet.has(relations?.target?.id);

  const handleSelectionChange = (keys) => {
    const selectableIds = new Set(
      relationRows.filter((item) => item.selectable).map((item) => item.id),
    );
    const nextIds = keys
      .map((key) => Number(key))
      .filter((id) => selectableIds.has(id));
    const targetId = relations?.target?.id;
    if (targetSelectable && !nextIds.includes(targetId)) {
      nextIds.unshift(targetId);
    }
    setSelectedRowKeys(nextIds);
  };

  const handleOk = async () => {
    if (!isDisable) {
      setSubmitting(true);
      try {
        await onConfirm();
      } finally {
        setSubmitting(false);
      }
      return;
    }
    if (step === 'details') {
      if (!canContinue) {
        return;
      }
      setReason(trimmedReason);
      setStep('confirm');
      return;
    }
    const targetId = relations?.target?.id;
    const relatedUserIds = selectedRowKeys
      .map((id) => Number(id))
      .filter((id) => id !== targetId);
    setSubmitting(true);
    try {
      await onConfirm(trimmedReason, relatedUserIds);
    } finally {
      setSubmitting(false);
    }
  };

  const handleCancel = () => {
    if (submitting) {
      return;
    }
    if (isDisable && step === 'confirm') {
      setStep('details');
      return;
    }
    onCancel();
  };

  const title = isDisable
    ? step === 'details'
      ? t('禁用用户及关联账号')
      : t('确认批量禁用用户？')
    : t('确定要启用此用户吗？');

  const columns = useMemo(
    () => [
      {
        title: t('关系'),
        dataIndex: 'relation_type',
        width: 110,
        render: (relationType) => {
          const config = getRelationTypeConfig(relationType, t);
          return (
            <Tag color={config.color} shape='circle'>
              {config.label}
            </Tag>
          );
        },
      },
      {
        title: t('用户 ID'),
        dataIndex: 'id',
        width: 90,
      },
      {
        title: t('用户名'),
        dataIndex: 'username',
        render: (username, record) => (
          <div className='flex flex-col'>
            <Text>{username}</Text>
            {record.display_name && record.display_name !== username ? (
              <Text type='tertiary' size='small'>
                {record.display_name}
              </Text>
            ) : null}
          </div>
        ),
      },
      {
        title: t('角色'),
        dataIndex: 'role',
        width: 100,
        render: (role) => {
          if (role === 100) {
            return t('超级管理员');
          }
          if (role === 10) {
            return t('管理员');
          }
          return t('普通用户');
        },
      },
      {
        title: t('状态'),
        dataIndex: 'status',
        width: 100,
        render: (status, record) => {
          if (record.deleted) {
            return (
              <Tag color='red' shape='circle'>
                {t('已注销')}
              </Tag>
            );
          }
          if (status === 2) {
            return (
              <Tag color='red' shape='circle'>
                {t('已禁用')}
              </Tag>
            );
          }
          return (
            <Tag color='green' shape='circle'>
              {t('已启用')}
            </Tag>
          );
        },
      },
      {
        title: t('可操作性'),
        dataIndex: 'selectable',
        width: 150,
        render: (selectable, record) =>
          selectable ? (
            <Text type='success'>{t('可封禁')}</Text>
          ) : (
            <Text type='tertiary'>
              {getUnavailableReasonLabel(record.unavailable_reason, t)}
            </Text>
          ),
      },
    ],
    [t],
  );

  const renderRelations = () => {
    if (relationsLoading) {
      return (
        <div className='flex min-h-[240px] items-center justify-center'>
          <Spin size='large' />
        </div>
      );
    }
    if (relationsError) {
      return (
        <Space vertical align='start' className='w-full'>
          <Banner
            type='danger'
            closeIcon={null}
            description={relationsError}
            className='w-full !rounded-lg'
          />
          <Button onClick={() => setReloadKey((key) => key + 1)}>
            {t('重试')}
          </Button>
        </Space>
      );
    }
    return (
      <Table
        size='small'
        rowKey='id'
        columns={columns}
        dataSource={relationRows}
        scroll={{ x: 'max-content', y: 320 }}
        pagination={
          relationRows.length > 10
            ? {
                pageSize: 10,
                showSizeChanger: false,
              }
            : false
        }
        rowSelection={{
          selectedRowKeys,
          onChange: handleSelectionChange,
          getCheckboxProps: (record) => ({
            disabled: record.relation_type === 'target' || !record.selectable,
          }),
        }}
      />
    );
  };

  const renderDisableDetailsStep = () => (
    <Space vertical align='start' className='w-full'>
      <Text>
        {t('目标用户固定选中；可封禁的直接邀请人和被邀请用户已默认选中。')}
      </Text>
      {renderRelations()}
      <Text>{t('请填写统一禁用原因，所选用户下次登录时将看到该原因。')}</Text>
      <TextArea
        value={reason}
        rows={4}
        maxLength={MAX_DISABLE_REASON_LENGTH}
        placeholder={t('请输入禁用原因')}
        onChange={(value) => setReason(value)}
        showClear
        style={{ width: '100%' }}
      />
      <Text type={trimmedReason ? 'tertiary' : 'danger'} size='small'>
        {trimmedReason
          ? `${reason.length}/${MAX_DISABLE_REASON_LENGTH}`
          : t('禁用原因不能为空')}
      </Text>
    </Space>
  );

  const renderDisableConfirmStep = () => (
    <Space vertical align='start' className='w-full'>
      <Paragraph>
        {t('本次将禁用 {{total}} 个用户，请确认选择范围和禁用原因。', {
          total: selectedRows.length,
        })}
      </Paragraph>
      <div className='grid w-full grid-cols-2 gap-2 md:grid-cols-4'>
        <div className='rounded-lg border border-semi-color-border p-3'>
          <Text type='tertiary' size='small'>
            {t('合计')}
          </Text>
          <div className='text-lg font-semibold'>{selectedRows.length}</div>
        </div>
        <div className='rounded-lg border border-semi-color-border p-3'>
          <Text type='tertiary' size='small'>
            {t('当前用户')}
          </Text>
          <div className='text-lg font-semibold'>{selectedCounts.target}</div>
        </div>
        <div className='rounded-lg border border-semi-color-border p-3'>
          <Text type='tertiary' size='small'>
            {t('邀请人')}
          </Text>
          <div className='text-lg font-semibold'>{selectedCounts.inviter}</div>
        </div>
        <div className='rounded-lg border border-semi-color-border p-3'>
          <Text type='tertiary' size='small'>
            {t('被邀请用户')}
          </Text>
          <div className='text-lg font-semibold'>{selectedCounts.invitee}</div>
        </div>
      </div>
      <div>
        <Text strong>{t('禁用原因')}：</Text>
        <Text>{trimmedReason}</Text>
      </div>
    </Space>
  );

  return (
    <Modal
      title={title}
      visible={visible}
      onCancel={handleCancel}
      onOk={handleOk}
      type='warning'
      width={800}
      confirmLoading={submitting}
      okText={isDisable && step === 'details' ? t('下一步') : t('确认')}
      cancelText={isDisable && step === 'confirm' ? t('上一步') : t('取消')}
      okButtonProps={{
        disabled:
          submitting ||
          (isDisable && step === 'details' && !canContinue) ||
          (isDisable && step === 'confirm' && selectedRows.length === 0),
      }}
    >
      {isDisable
        ? step === 'details'
          ? renderDisableDetailsStep()
          : renderDisableConfirmStep()
        : t('此操作将启用用户账户')}
    </Modal>
  );
};

export default EnableDisableUserModal;
