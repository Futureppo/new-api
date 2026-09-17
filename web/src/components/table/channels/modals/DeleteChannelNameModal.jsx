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

import React, { useEffect, useState } from 'react';
import { Banner, Input, Modal, Typography } from '@douyinfe/semi-ui';

const DeleteChannelNameModal = ({
  showDeleteChannelName,
  closeDeleteChannelName,
  deleteChannelNameLoading,
  deleteChannelsByName,
  t,
}) => {
  const [name, setName] = useState('');

  useEffect(() => {
    setName('');
  }, [showDeleteChannelName]);

  const valid = name.trim() !== '';

  return (
    <Modal
      title={t('按名称删除渠道')}
      visible={showDeleteChannelName}
      onOk={() => deleteChannelsByName(name)}
      onCancel={closeDeleteChannelName}
      okText={t('确认删除')}
      cancelText={t('取消')}
      okButtonProps={{
        type: 'danger',
        disabled: !valid || deleteChannelNameLoading,
        'aria-label': t('确认删除'),
      }}
      cancelButtonProps={{
        disabled: deleteChannelNameLoading,
        'aria-label': t('取消'),
      }}
      confirmLoading={deleteChannelNameLoading}
      closable={!deleteChannelNameLoading}
      closeOnEsc={!deleteChannelNameLoading}
      maskClosable={false}
      centered
      size='small'
      className='!rounded-lg'
    >
      <Banner
        type='warning'
        closeIcon={null}
        description={t(
          '将删除名称完全相同的所有启用和禁用渠道，不受搜索、筛选和分页影响。删除后无法恢复。',
        )}
      />
      <div className='my-4'>
        <label htmlFor='channel-delete-name'>{t('渠道名称')}</label>
        <Input
          id='channel-delete-name'
          value={name}
          onChange={setName}
          placeholder={t('请输入完整渠道名称')}
          aria-required={true}
          disabled={deleteChannelNameLoading}
        />
        <Typography.Text type='secondary'>
          {t('精确匹配，区分大小写和前后空格，不支持模糊匹配或通配符。')}
        </Typography.Text>
      </div>
      {valid ? (
        <Typography.Text
          type='danger'
          aria-live='polite'
          style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}
        >
          {t('将删除名称为「{{name}}」的所有渠道', { name })}
        </Typography.Text>
      ) : null}
    </Modal>
  );
};

export default DeleteChannelNameModal;
