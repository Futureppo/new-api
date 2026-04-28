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
import { Modal, Space, TextArea, Typography } from '@douyinfe/semi-ui';

const { Text, Paragraph } = Typography;

const MAX_DISABLE_REASON_LENGTH = 255;

const EnableDisableUserModal = ({
  visible,
  onCancel,
  onConfirm,
  user,
  action,
  t,
}) => {
  const isDisable = action === 'disable';
  const [step, setStep] = useState('reason');
  const [reason, setReason] = useState('');

  useEffect(() => {
    if (visible) {
      setStep(isDisable ? 'reason' : 'confirm');
      setReason('');
    }
  }, [visible, isDisable, user?.id]);

  const trimmedReason = reason.trim();

  const handleOk = () => {
    if (!isDisable) {
      onConfirm();
      return;
    }
    if (step === 'reason') {
      if (!trimmedReason) {
        return;
      }
      setReason(trimmedReason);
      setStep('confirm');
      return;
    }
    onConfirm(trimmedReason);
  };

  const title = isDisable
    ? step === 'reason'
      ? t('填写禁用原因')
      : t('确认禁用此用户？')
    : t('确定要启用此用户吗？');

  const renderDisableReasonStep = () => (
    <Space vertical align='start' className='w-full'>
      <Text>{t('请填写禁用原因，用户下次登录时将看到该原因。')}</Text>
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
      <Paragraph>{t('此操作将禁用用户账户')}</Paragraph>
      <div>
        <Text strong>{t('用户 ID')}：</Text>
        <Text>{user?.id}</Text>
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
      onCancel={onCancel}
      onOk={handleOk}
      type='warning'
      okText={isDisable && step === 'reason' ? t('下一步') : t('确认')}
      cancelText={t('取消')}
      okButtonProps={{
        disabled: isDisable && step === 'reason' && !trimmedReason,
      }}
    >
      {isDisable
        ? step === 'reason'
          ? renderDisableReasonStep()
          : renderDisableConfirmStep()
        : t('此操作将启用用户账户')}
    </Modal>
  );
};

export default EnableDisableUserModal;
