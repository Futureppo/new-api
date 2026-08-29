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

import React, { useState } from 'react';
import {
  Button,
  Dropdown,
  Modal,
  Space,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import {
  ChevronDown,
  Trash2,
  UserCheck,
  UserMinus,
  UserPlus,
  UserX,
} from 'lucide-react';

const { Paragraph, Text } = Typography;
const MAX_DISABLE_REASON_LENGTH = 255;

const UsersActions = ({
  setShowAddUser,
  batchManageUsers,
  purgeSoftDeletedUsers,
  batchActionLoading,
  t,
}) => {
  const [pendingAction, setPendingAction] = useState('');
  const [disableReason, setDisableReason] = useState('');
  const [reasonError, setReasonError] = useState(false);

  // Add new user
  const handleAddUser = () => {
    setShowAddUser(true);
  };

  const openBatchAction = (action) => {
    setDisableReason('');
    setReasonError(false);
    setPendingAction(action);
  };

  const closeBatchAction = () => {
    if (!batchActionLoading) {
      setPendingAction('');
    }
  };

  const actionConfig = {
    enable_disabled: {
      title: t('确定启用所有已禁用普通用户？'),
      description: t('将启用全站所有已禁用的普通用户，并清空其禁用原因。'),
      okText: t('确认启用'),
      danger: false,
    },
    disable_enabled: {
      title: t('确定禁用所有已启用普通用户？'),
      description: t('请填写统一禁用原因，将禁用全站所有已启用的普通用户。'),
      okText: t('确认禁用'),
      danger: true,
    },
    delete_disabled: {
      title: t('确定注销所有已禁用普通用户？'),
      description: t('将软删除全站所有已禁用的普通用户，用户将无法登录。'),
      okText: t('确认注销'),
      danger: true,
    },
    purge_disabled: {
      title: t('确定永久清理所有已禁用普通用户？'),
      description: t(
        '将从数据库永久删除所有已禁用的普通用户，此操作不可撤销。',
      ),
      okText: t('确认清理'),
      danger: true,
    },
    purge_soft_deleted: {
      title: t('确定永久清理所有已注销普通用户？'),
      description: t(
        '将从数据库永久删除所有已注销的普通用户，此操作不可撤销。',
      ),
      okText: t('确认清理'),
      danger: true,
    },
  };

  const currentAction = actionConfig[pendingAction];

  const handleConfirmBatchAction = async () => {
    const reason = disableReason.trim();
    if (pendingAction === 'disable_enabled' && !reason) {
      setReasonError(true);
      return false;
    }

    const succeeded =
      pendingAction === 'purge_soft_deleted'
        ? await purgeSoftDeletedUsers()
        : await batchManageUsers(pendingAction, reason);
    if (succeeded) {
      setPendingAction('');
    }
    return succeeded;
  };

  const isLoading = Boolean(batchActionLoading);

  return (
    <>
      <div className='flex flex-wrap gap-2 w-full md:w-auto order-2 md:order-1'>
        <Button
          className='flex-1 md:flex-initial'
          disabled={isLoading}
          icon={<UserPlus size={14} />}
          onClick={handleAddUser}
          size='small'
        >
          {t('添加用户')}
        </Button>
        <Dropdown
          position='bottomLeft'
          render={
            <Dropdown.Menu>
              <Dropdown.Item
                icon={<UserCheck size={14} />}
                onClick={() => openBatchAction('enable_disabled')}
              >
                {t('启用全部已禁用用户')}
              </Dropdown.Item>
              <Dropdown.Item
                icon={<UserX size={14} />}
                onClick={() => openBatchAction('disable_enabled')}
              >
                {t('禁用全部已启用用户')}
              </Dropdown.Item>
              <Dropdown.Item
                icon={<UserMinus size={14} />}
                onClick={() => openBatchAction('delete_disabled')}
              >
                {t('注销全部已禁用用户')}
              </Dropdown.Item>
              <Dropdown.Divider />
              <Dropdown.Item
                icon={<Trash2 size={14} />}
                onClick={() => openBatchAction('purge_disabled')}
              >
                <span style={{ color: 'var(--semi-color-danger)' }}>
                  {t('永久清理已禁用用户')}
                </span>
              </Dropdown.Item>
              <Dropdown.Item
                icon={<Trash2 size={14} />}
                onClick={() => openBatchAction('purge_soft_deleted')}
              >
                <span style={{ color: 'var(--semi-color-danger)' }}>
                  {t('永久清理已注销用户')}
                </span>
              </Dropdown.Item>
            </Dropdown.Menu>
          }
          trigger='click'
        >
          <Button
            className='flex-1 md:flex-initial'
            disabled={isLoading}
            icon={<ChevronDown size={14} />}
            loading={isLoading}
            size='small'
            type='tertiary'
          >
            {t('批量操作')}
          </Button>
        </Dropdown>
      </div>

      <Modal
        cancelButtonProps={{ disabled: isLoading }}
        cancelText={t('取消')}
        centered
        closeOnEsc={!isLoading}
        confirmLoading={isLoading}
        maskClosable={false}
        okButtonProps={{ type: currentAction?.danger ? 'danger' : 'primary' }}
        okText={currentAction?.okText}
        onCancel={closeBatchAction}
        onOk={handleConfirmBatchAction}
        title={currentAction?.title}
        visible={Boolean(pendingAction)}
      >
        {currentAction && (
          <Space vertical align='start' className='w-full'>
            <Paragraph>{currentAction.description}</Paragraph>
            <Text type='secondary'>
              {t(
                '所有批量操作仅处理普通用户，管理员和超级管理员不会受到影响。',
              )}
            </Text>
            {pendingAction === 'disable_enabled' && (
              <div className='w-full'>
                <TextArea
                  maxLength={MAX_DISABLE_REASON_LENGTH}
                  onChange={(value) => {
                    setDisableReason(value);
                    if (value.trim()) {
                      setReasonError(false);
                    }
                  }}
                  placeholder={t('请输入禁用原因')}
                  rows={4}
                  showClear
                  style={{ width: '100%' }}
                  value={disableReason}
                />
                <div className='mt-1 flex justify-between'>
                  <Text type={reasonError ? 'danger' : 'tertiary'} size='small'>
                    {reasonError ? t('禁用原因不能为空') : ' '}
                  </Text>
                  <Text type='tertiary' size='small'>
                    {disableReason.length}/{MAX_DISABLE_REASON_LENGTH}
                  </Text>
                </div>
              </div>
            )}
          </Space>
        )}
      </Modal>
    </>
  );
};

export default UsersActions;
