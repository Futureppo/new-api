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

import React from 'react';
import { Button, Input, Modal, Typography } from '@douyinfe/semi-ui';
import { IconLock } from '@douyinfe/semi-icons';
import {
  getPasswordChecks,
  isValidLoginPassword,
} from '../../../../helpers/password';

const ChangePasswordModal = ({
  t,
  showChangePasswordModal,
  setShowChangePasswordModal,
  inputs,
  handleInputChange,
  changePassword,
  hasPassword = true,
  loading = false,
  error = '',
  onLogout,
}) => {
  const required = !hasPassword;
  const checks = getPasswordChecks(inputs.set_new_password);
  const valid =
    isValidLoginPassword(inputs.set_new_password) &&
    inputs.set_new_password === inputs.set_new_password_confirmation &&
    (!hasPassword || Boolean(inputs.original_password));
  const labels = {
    length: t('至少 8 个字符'),
    bytes: t('最多 72 个 UTF-8 字节'),
    digit: t('包含数字（0-9）'),
    upper: t('包含大写字母（A-Z）'),
    lower: t('包含小写字母（a-z）'),
    symbol: t('包含英文标点符号（如 !@#$%）'),
  };
  return (
    <Modal
      title={
        <div className='flex items-center'>
          <IconLock className='mr-2 text-orange-500' />
          {hasPassword ? t('修改密码') : t('设置登录密码')}
        </div>
      }
      visible={showChangePasswordModal || required}
      closable={!required && !loading}
      maskClosable={false}
      closeOnEsc={!required && !loading}
      onCancel={() =>
        !required && !loading && setShowChangePasswordModal(false)
      }
      footer={
        <div className='flex justify-between gap-3'>
          {required ? (
            <Button onClick={onLogout} disabled={loading}>
              {t('退出登录')}
            </Button>
          ) : (
            <Button
              onClick={() => setShowChangePasswordModal(false)}
              disabled={loading}
            >
              {t('取消')}
            </Button>
          )}
          <Button
            theme='solid'
            type='primary'
            onClick={changePassword}
            disabled={!valid}
            loading={loading}
          >
            {hasPassword ? t('修改密码') : t('设置登录密码')}
          </Button>
        </div>
      }
      size='small'
      centered
      className='modern-modal'
    >
      <div className='space-y-4 py-4'>
        {required && (
          <Typography.Paragraph>
            {t('为保障账户安全，请先设置登录密码后继续使用。')}
          </Typography.Paragraph>
        )}
        {hasPassword && (
          <div>
            <Typography.Text strong className='block mb-2'>
              {t('原密码')}
            </Typography.Text>
            <Input
              name='original_password'
              aria-label={t('原密码')}
              placeholder={t('请输入原密码')}
              mode='password'
              autoComplete='current-password'
              value={inputs.original_password}
              onChange={(value) =>
                handleInputChange('original_password', value)
              }
              size='large'
              prefix={<IconLock />}
              disabled={loading}
            />
          </div>
        )}
        <div>
          <Typography.Text strong className='block mb-2'>
            {t('新密码')}
          </Typography.Text>
          <Input
            name='set_new_password'
            aria-label={t('新密码')}
            placeholder={t('请输入新密码')}
            mode='password'
            autoComplete='new-password'
            value={inputs.set_new_password}
            onChange={(value) => handleInputChange('set_new_password', value)}
            size='large'
            prefix={<IconLock />}
            disabled={loading}
          />
        </div>
        <ul className='text-sm space-y-1' aria-live='polite'>
          {Object.entries(checks).map(([key, passed]) => (
            <li
              key={key}
              className={passed ? 'text-green-600' : 'text-gray-500'}
            >
              {passed ? '✓' : '○'} {labels[key]}
            </li>
          ))}
        </ul>
        <div>
          <Typography.Text strong className='block mb-2'>
            {t('确认新密码')}
          </Typography.Text>
          <Input
            name='set_new_password_confirmation'
            aria-label={t('确认新密码')}
            placeholder={t('请再次输入新密码')}
            mode='password'
            autoComplete='new-password'
            value={inputs.set_new_password_confirmation}
            onChange={(value) =>
              handleInputChange('set_new_password_confirmation', value)
            }
            size='large'
            prefix={<IconLock />}
            disabled={loading}
          />
          {inputs.set_new_password_confirmation &&
            inputs.set_new_password !==
              inputs.set_new_password_confirmation && (
              <Typography.Text type='danger'>
                {t('两次输入的密码不一致！')}
              </Typography.Text>
            )}
        </div>
        {error && (
          <div role='alert'>
            <Typography.Text type='danger'>{error}</Typography.Text>
          </div>
        )}
      </div>
    </Modal>
  );
};

export default ChangePasswordModal;
