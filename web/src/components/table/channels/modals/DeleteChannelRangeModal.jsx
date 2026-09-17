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
import { parseChannelIdRange } from '../../../../helpers/channelRange';

const DeleteChannelRangeModal = ({
  showDeleteChannelRange,
  closeDeleteChannelRange,
  deleteChannelRangeLoading,
  deleteChannelsByRange,
  t,
}) => {
  const [startId, setStartId] = useState('');
  const [endId, setEndId] = useState('');

  useEffect(() => {
    setStartId('');
    setEndId('');
  }, [showDeleteChannelRange]);

  const range = parseChannelIdRange(startId, endId);

  return (
    <Modal
      title={t('按 ID 范围删除渠道')}
      visible={showDeleteChannelRange}
      onOk={() => deleteChannelsByRange(startId, endId)}
      onCancel={closeDeleteChannelRange}
      okText={t('确认删除')}
      cancelText={t('取消')}
      okButtonProps={{
        type: 'danger',
        disabled: !range || deleteChannelRangeLoading,
        'aria-label': t('确认删除'),
      }}
      cancelButtonProps={{
        disabled: deleteChannelRangeLoading,
        'aria-label': t('取消'),
      }}
      confirmLoading={deleteChannelRangeLoading}
      closable={!deleteChannelRangeLoading}
      closeOnEsc={!deleteChannelRangeLoading}
      maskClosable={false}
      centered
      size='small'
      className='!rounded-lg'
    >
      <Banner
        type='warning'
        closeIcon={null}
        description={t(
          '将删除范围内所有启用和禁用渠道，不受搜索、筛选和分页影响。删除后无法恢复。',
        )}
      />
      <div className='my-4 flex flex-col gap-4'>
        <div>
          <label htmlFor='channel-range-start'>{t('起始 ID')}</label>
          <Input
            id='channel-range-start'
            value={startId}
            onChange={setStartId}
            inputMode='numeric'
            aria-required={true}
            disabled={deleteChannelRangeLoading}
          />
        </div>
        <div>
          <label htmlFor='channel-range-end'>{t('结束 ID')}</label>
          <Input
            id='channel-range-end'
            value={endId}
            onChange={setEndId}
            inputMode='numeric'
            aria-required={true}
            disabled={deleteChannelRangeLoading}
          />
        </div>
      </div>
      <Typography.Text type={range ? 'danger' : 'secondary'} aria-live='polite'>
        {range
          ? t('将删除 ID {{start}} 至 {{end}} 的渠道（包含两端）', {
              start: range.start_id,
              end: range.end_id,
            })
          : t(
              '请输入不超过 9007199254740991 的正整数，且起始 ID 不能大于结束 ID',
            )}
      </Typography.Text>
    </Modal>
  );
};

export default DeleteChannelRangeModal;
