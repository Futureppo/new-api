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
import { Card, Select, Typography, Button, Switch } from '@douyinfe/semi-ui';
import { Sparkles, Users, ToggleLeft, X, Settings, Route } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { renderGroupOption, selectFilter } from '../../helpers';
import ParameterControl from './ParameterControl';
import ImageUrlInput from './ImageUrlInput';
import ConfigManager from './ConfigManager';
import CustomRequestEditor from './CustomRequestEditor';
import { isPlaygroundChat } from '../../helpers/playgroundEndpoints';

const SettingsPanel = ({
  inputs,
  parameterEnabled,
  models,
  groups,
  endpoint = inputs.endpoint || '',
  endpointOptions,
  endpointReady,
  endpointLoading,
  isGenerating,
  onSubmitNativeChat,
  nativeTemplate,
  onNativeDraftChange,
  styleState,
  showDebugPanel,
  customRequestMode,
  customRequestBody,
  onInputChange,
  onParameterToggle,
  onCloseSettings,
  onConfigImport,
  onConfigReset,
  onCustomRequestModeChange,
  onCustomRequestBodyChange,
  previewPayload,
  messages,
}) => {
  const { t } = useTranslation();
  const isChat = isPlaygroundChat(endpoint);
  const legacyCustom = !endpoint && customRequestMode;
  const nativeChat = !!endpoint && isChat && customRequestMode;
  const disabledParameters = legacyCustom || nativeChat;

  const currentConfig = {
    inputs,
    parameterEnabled,
    showDebugPanel,
    customRequestMode,
    customRequestBody,
  };

  return (
    <Card
      className='h-full flex flex-col'
      bordered={false}
      bodyStyle={{
        padding: styleState.isMobile ? '16px' : '24px',
        height: '100%',
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      {/* 标题区域 - 与调试面板保持一致 */}
      <div className='flex items-center justify-between mb-6 flex-shrink-0'>
        <div className='flex items-center'>
          <div className='w-10 h-10 rounded-full bg-gradient-to-r from-purple-500 to-pink-500 flex items-center justify-center mr-3'>
            <Settings size={20} className='text-white' />
          </div>
          <Typography.Title heading={5} className='mb-0'>
            {t('模型配置')}
          </Typography.Title>
        </div>

        {styleState.isMobile && onCloseSettings && (
          <Button
            icon={<X size={16} />}
            onClick={onCloseSettings}
            theme='borderless'
            type='tertiary'
            size='small'
            className='!rounded-lg'
          />
        )}
      </div>

      {/* 移动端配置管理 */}
      {styleState.isMobile && (
        <div className='mb-4 flex-shrink-0'>
          <ConfigManager
            currentConfig={currentConfig}
            onConfigImport={onConfigImport}
            onConfigReset={onConfigReset}
            styleState={{ ...styleState, isMobile: false }}
            messages={messages}
          />
        </div>
      )}

      <div className='space-y-6 overflow-y-auto flex-1 pr-2 model-settings-scroll'>
        {/* 自定义请求体编辑器 */}
        {!endpoint && (
          <CustomRequestEditor
            customRequestMode={customRequestMode}
            customRequestBody={customRequestBody}
            onCustomRequestModeChange={onCustomRequestModeChange}
            onCustomRequestBodyChange={onCustomRequestBodyChange}
            defaultPayload={previewPayload}
          />
        )}

        {/* 分组选择 */}
        <div className={legacyCustom ? 'opacity-50' : ''}>
          <div className='flex items-center gap-2 mb-2'>
            <Users size={16} className='text-gray-500' />
            <Typography.Text strong className='text-sm'>
              {t('分组')}
            </Typography.Text>
            {legacyCustom && (
              <Typography.Text className='text-xs text-orange-600'>
                ({t('已在自定义模式中忽略')})
              </Typography.Text>
            )}
          </div>
          <Select
            placeholder={t('请选择分组')}
            name='group'
            required
            selection
            filter={selectFilter}
            autoClearSearchValue={false}
            onChange={(value) => onInputChange('group', value)}
            value={inputs.group}
            autoComplete='new-password'
            optionList={groups}
            renderOptionItem={renderGroupOption}
            style={{ width: '100%' }}
            dropdownStyle={{ width: '100%', maxWidth: '100%' }}
            className='!rounded-lg'
            disabled={legacyCustom || isGenerating}
          />
        </div>

        {/* 模型选择 */}
        <div className={legacyCustom ? 'opacity-50' : ''}>
          <div className='flex items-center gap-2 mb-2'>
            <Sparkles size={16} className='text-gray-500' />
            <Typography.Text strong className='text-sm'>
              {t('模型')}
            </Typography.Text>
            {legacyCustom && (
              <Typography.Text className='text-xs text-orange-600'>
                ({t('已在自定义模式中忽略')})
              </Typography.Text>
            )}
          </div>
          <Select
            placeholder={t('请选择模型')}
            name='model'
            required
            selection
            filter={selectFilter}
            autoClearSearchValue={false}
            onChange={(value) => onInputChange('model', value)}
            value={inputs.model}
            autoComplete='new-password'
            optionList={models}
            style={{ width: '100%' }}
            dropdownStyle={{ width: '100%', maxWidth: '100%' }}
            className='!rounded-lg'
            disabled={legacyCustom || isGenerating}
          />
        </div>

        <div className='space-y-2'>
          <Typography.Text strong className='flex items-center gap-2 text-sm'>
            <Route size={16} className='text-gray-500' /> {t('上游路径')}
          </Typography.Text>
          <Select
            aria-label={t('上游路径')}
            value={endpoint}
            onChange={(value) => onInputChange('endpoint', value)}
            optionList={endpointOptions}
            disabled={isGenerating}
            loading={endpointLoading}
            style={{ width: '100%' }}
            dropdownStyle={{ maxWidth: 'min(90vw, 480px)' }}
          />
          {inputs.model && !endpointLoading && !endpointReady && (
            <Typography.Text type='warning' className='text-xs'>
              {t('当前模型没有支持所选路径的可用渠道')}
            </Typography.Text>
          )}
        </div>

        {endpoint && isChat && (
          <div className='space-y-3'>
            <CustomRequestEditor
              key={endpoint}
              native
              customRequestMode={customRequestMode}
              customRequestBody={inputs.nativeDrafts?.[endpoint] ?? ''}
              onCustomRequestModeChange={onCustomRequestModeChange}
              onCustomRequestBodyChange={onNativeDraftChange}
              defaultPayload={nativeTemplate}
            />
            {customRequestMode && (
              <Button
                theme='solid'
                type='primary'
                disabled={!endpointReady || isGenerating}
                onClick={onSubmitNativeChat}
              >
                {t('发送请求')}
              </Button>
            )}
          </div>
        )}

        {isChat && (
          <div className={disabledParameters ? 'opacity-50' : ''}>
            <ImageUrlInput
              imageUrls={inputs.imageUrls}
              imageEnabled={inputs.imageEnabled}
              onImageUrlsChange={(urls) => onInputChange('imageUrls', urls)}
              onImageEnabledChange={(enabled) =>
                onInputChange('imageEnabled', enabled)
              }
              disabled={disabledParameters}
            />
          </div>
        )}

        {/* 参数控制组件 */}
        {isChat && (
          <div className={disabledParameters ? 'opacity-50' : ''}>
            <ParameterControl
              inputs={inputs}
              parameterEnabled={parameterEnabled}
              onInputChange={onInputChange}
              onParameterToggle={onParameterToggle}
              disabled={disabledParameters}
            />
          </div>
        )}

        {/* 流式输出开关 */}
        {isChat && (
          <div
            className={
              disabledParameters && !(nativeChat && endpoint === 'gemini')
                ? 'opacity-50'
                : ''
            }
          >
            <div className='flex items-center justify-between'>
              <div className='flex items-center gap-2'>
                <ToggleLeft size={16} className='text-gray-500' />
                <Typography.Text strong className='text-sm'>
                  {t('流式输出')}
                </Typography.Text>
                {disabledParameters &&
                  !(nativeChat && endpoint === 'gemini') && (
                    <Typography.Text className='text-xs text-orange-600'>
                      ({t('已在自定义模式中忽略')})
                    </Typography.Text>
                  )}
              </div>
              <Switch
                checked={
                  inputs.stream &&
                  endpointOptions.find((item) => item.value === endpoint)
                    ?.stream !== false
                }
                onChange={(checked) => onInputChange('stream', checked)}
                checkedText={t('开')}
                uncheckedText={t('关')}
                size='small'
                disabled={
                  isGenerating ||
                  (disabledParameters &&
                    !(nativeChat && endpoint === 'gemini')) ||
                  endpointOptions.find((item) => item.value === endpoint)
                    ?.stream === false
                }
              />
            </div>
          </div>
        )}
      </div>

      {/* 桌面端的配置管理放在底部 */}
      {!styleState.isMobile && (
        <div className='flex-shrink-0 pt-3'>
          <ConfigManager
            currentConfig={currentConfig}
            onConfigImport={onConfigImport}
            onConfigReset={onConfigReset}
            styleState={styleState}
            messages={messages}
          />
        </div>
      )}
    </Card>
  );
};

export default SettingsPanel;
