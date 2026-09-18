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
import { InputNumber, Typography } from '@douyinfe/semi-ui';
import { timestamp2string } from '../../helpers';

export const isDisableDurationValid = (value) => {
  if (value === '' || value === null || value === undefined) return false;
  const minutes = Number(value);
  return (
    Number.isSafeInteger(minutes) &&
    minutes >= 0 &&
    minutes <= Math.floor((253402300799 - Math.floor(Date.now() / 1000)) / 60)
  );
};

export const disableDurationText = (minutes, t) =>
  Number(minutes) === 0
    ? t('永久封禁')
    : t('封禁 {{minutes}} 分钟', { minutes });

export const userDisableTimeText = (user, t) => {
  if (!user || Number(user.status) !== 2) return '';
  if (!user.disable_until) return t('永久封禁');
  return `${disableDurationText(user.disable_duration_minutes, t)} · ${t('解禁时间')}：${timestamp2string(user.disable_until)}`;
};

export function DisableDurationInput({ value, onChange, t, disabled = false }) {
  const valid = isDisableDurationValid(value);
  return (
    <div className='flex w-full flex-col gap-1'>
      <Typography.Text strong>{t('禁用时长（分钟）')}</Typography.Text>
      <InputNumber
        value={value}
        onChange={onChange}
        step={1}
        disabled={disabled}
        aria-label={t('禁用时长（分钟）')}
        style={{ width: 200 }}
      />
      <Typography.Text size='small' type={valid ? 'tertiary' : 'danger'}>
        {valid
          ? t('0 为永久封禁，到期自动解禁')
          : t('禁用时长必须为有效范围内的非负整数')}
      </Typography.Text>
    </div>
  );
}

export function UserDisableInfo({ user, t }) {
  const text = userDisableTimeText(user, t);
  return text ? (
    <div className='text-xs text-red-600 dark:text-red-400 break-words'>
      {text}
    </div>
  ) : null;
}
