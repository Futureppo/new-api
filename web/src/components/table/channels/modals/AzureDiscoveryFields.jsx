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

import React, { useId } from 'react';
import { Button, Input, Tag, Typography } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';

const { Text } = Typography;

export const AzureModelDiscoveryStatus = ({ discovery }) => {
  const { t } = useTranslation();
  const result = discovery.result;
  return (
    <div className='mt-2 space-y-1' aria-live='polite'>
      {result?.model_source && (
        <Tag color='blue'>
          {result.model_source === 'deployments'
            ? t('已部署模型')
            : t('可用模型目录')}
        </Tag>
      )}
      {result?.model_source === 'catalog' && (
        <div>
          <Text type='tertiary' size='small'>
            {t(
              '目录包含基础模型，不会自动填入；请确认已部署后选择或填写部署名称。',
            )}
          </Text>
        </div>
      )}
      {result?.model_source === 'deployments' &&
        !result.model_error &&
        result.models?.length === 0 && (
          <div>
            <Text type='tertiary' size='small'>
              {t('暂无已部署模型')}
            </Text>
          </div>
        )}
      {result?.management_fallback && (
        <div>
          <Text type='warning' size='small'>
            {result.model_source === 'catalog'
              ? t('Azure 管理授权不可用，已切换为模型目录。')
              : t('Azure 区域管理授权不可用，尝试从响应头获取区域。')}
          </Text>
        </div>
      )}
      {result?.model_error && (
        <div>
          <Text type='danger' size='small'>
            {result.model_error}
          </Text>
        </div>
      )}
      {!discovery.permitted && (
        <div>
          <Text type='tertiary' size='small'>
            {t(
              '获取未保存的 Azure 配置需要超级管理员权限；可保存渠道后重新打开获取。',
            )}
          </Text>
        </div>
      )}
    </div>
  );
};

const AzureDiscoveryFields = ({
  inputs,
  discovery,
  onResourceIDChange,
  canOverride,
}) => {
  const { t } = useTranslation();
  const loadingRegion = discovery.busy === 'all' || discovery.busy === 'region';
  const regionId = useId();
  const resourceId = useId();
  const tokenId = useId();
  return (
    <div className='my-4 space-y-3'>
      <label className='block font-semibold' htmlFor={regionId}>
        {t('区域识别')}
      </label>
      <div className='flex flex-wrap items-center gap-2'>
        <Input
          id={regionId}
          className='min-w-0 flex-1'
          style={{ minWidth: 150 }}
          value={inputs.azure_region || ''}
          placeholder={loadingRegion ? t('正在获取区域') : t('未识别到区域')}
          readonly
        />
        <Button
          disabled={!discovery.enabled || !!discovery.busy}
          loading={loadingRegion}
          onClick={() => discovery.discover('region')}
        >
          {t('获取区域')}
        </Button>
      </div>
      <div aria-live='polite'>
        {inputs.azure_region && (
          <Text type='tertiary' size='small'>
            {inputs.azure_region_source === 'resource'
              ? t('资源区域')
              : t('响应头区域')}
          </Text>
        )}
        {discovery.result?.region_error && (
          <div>
            <Text type='danger' size='small'>
              {discovery.result.region_error}
            </Text>
          </div>
        )}
      </div>
      {canOverride && (
        <details>
          <summary className='cursor-pointer text-sm'>
            {t('Azure 管理授权（可选）')}
          </summary>
          <div className='mt-3 space-y-3'>
            <label className='block' htmlFor={resourceId}>
              {t('Azure 资源 ID')}
            </label>
            <Input
              id={resourceId}
              value={inputs.azure_resource_id || ''}
              onChange={onResourceIDChange}
              placeholder='/subscriptions/.../resourceGroups/.../providers/Microsoft.CognitiveServices/accounts/...'
            />
            <label className='block' htmlFor={tokenId}>
              {t('临时管理令牌')}
            </label>
            <Input
              id={tokenId}
              mode='password'
              autoComplete='off'
              value={discovery.managementToken}
              onChange={discovery.setManagementToken}
              placeholder={t('Azure 管理访问令牌')}
            />
            <Text type='tertiary' size='small'>
              {t('用于读取已部署模型和资源区域，令牌仅在当前窗口使用。')}
            </Text>
          </div>
        </details>
      )}
    </div>
  );
};

export default AzureDiscoveryFields;
