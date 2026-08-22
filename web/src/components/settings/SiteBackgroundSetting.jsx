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

import React, { useContext, useEffect, useMemo, useState } from 'react';
import {
  Banner,
  Button,
  Card,
  Input,
  InputNumber,
  Select,
  Space,
  Spin,
  Switch,
  Typography,
} from '@douyinfe/semi-ui';
import { IconDelete, IconPlus } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../helpers';
import { StatusContext } from '../../context/Status';
import {
  DEFAULT_SITE_BACKGROUND_CONFIG,
  isAllowedSiteBackgroundURL,
  resolveSiteBackground,
  resolveSiteBackgroundGlassVeil,
  SITE_BACKGROUND_FIT_MODES,
  SITE_BACKGROUND_MAX_SOURCES,
  SITE_BACKGROUND_OPTION_KEY,
  SITE_BACKGROUND_SOURCE_TYPES,
} from '../../services/siteBackground';
import SiteBackgroundGlassFilter, {
  darkVariantFilterId,
  SITE_BACKGROUND_GLASS_PREVIEW_FILTER_ID,
} from '../layout/SiteBackgroundGlassFilter';

const JSON_PATH_PATTERN = /^[A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)*$/;

const parseDraftSource = (source) => {
  const weight = Number(source?.weight);
  return {
    type: Object.values(SITE_BACKGROUND_SOURCE_TYPES).includes(source?.type)
      ? source.type
      : SITE_BACKGROUND_SOURCE_TYPES.IMAGE_URL,
    url: String(source?.url || ''),
    json_path: String(source?.json_path || ''),
    enabled: source?.enabled !== false,
    weight: Number.isFinite(weight)
      ? Math.min(100, Math.max(1, Math.round(weight)))
      : 1,
  };
};

const parseDraftConfig = (value) => {
  let parsed = value;
  if (typeof value === 'string') {
    try {
      parsed = JSON.parse(value);
    } catch {
      parsed = {};
    }
  }
  if (!parsed || typeof parsed !== 'object') parsed = {};

  const opacity = Number(parsed.overlay_opacity);
  const glassOpacity = Number(parsed.glass_opacity);
  const glassRefraction = Number(parsed.glass_refraction);
  return {
    enabled: parsed.enabled === true,
    fit: SITE_BACKGROUND_FIT_MODES.includes(parsed.fit)
      ? parsed.fit
      : DEFAULT_SITE_BACKGROUND_CONFIG.fit,
    overlay_opacity: Number.isFinite(opacity)
      ? Math.min(80, Math.max(0, Math.round(opacity)))
      : DEFAULT_SITE_BACKGROUND_CONFIG.overlay_opacity,
    glass_enabled: parsed.glass_enabled === true,
    glass_opacity: Number.isFinite(glassOpacity)
      ? Math.min(100, Math.max(0, Math.round(glassOpacity)))
      : DEFAULT_SITE_BACKGROUND_CONFIG.glass_opacity,
    glass_refraction: Number.isFinite(glassRefraction)
      ? Math.min(100, Math.max(0, Math.round(glassRefraction)))
      : DEFAULT_SITE_BACKGROUND_CONFIG.glass_refraction,
    sources: Array.isArray(parsed.sources)
      ? parsed.sources
          .slice(0, SITE_BACKGROUND_MAX_SOURCES)
          .map(parseDraftSource)
      : [],
  };
};

const cleanDraftConfig = (draft) => ({
  enabled: draft.enabled === true,
  fit: draft.fit,
  overlay_opacity: Number(draft.overlay_opacity),
  glass_enabled: draft.glass_enabled === true,
  glass_opacity: Number(draft.glass_opacity),
  glass_refraction: Number(draft.glass_refraction),
  sources: draft.sources.map((source) => ({
    type: source.type,
    url: source.url.trim(),
    enabled: source.enabled !== false,
    weight: Number(source.weight),
    ...(source.type === SITE_BACKGROUND_SOURCE_TYPES.JSON_API
      ? { json_path: source.json_path.trim() }
      : {}),
  })),
});

const validateDraftConfig = (config, t) => {
  if (!SITE_BACKGROUND_FIT_MODES.includes(config.fit)) {
    return t('请选择有效的背景显示模式');
  }
  if (
    !Number.isInteger(config.overlay_opacity) ||
    config.overlay_opacity < 0 ||
    config.overlay_opacity > 80
  ) {
    return t('背景遮罩强度必须是 0 到 80 之间的整数');
  }
  if (
    !Number.isInteger(config.glass_opacity) ||
    config.glass_opacity < 0 ||
    config.glass_opacity > 100
  ) {
    return t('玻璃不透明度必须是 0 到 100 之间的整数');
  }
  if (
    !Number.isInteger(config.glass_refraction) ||
    config.glass_refraction < 0 ||
    config.glass_refraction > 100
  ) {
    return t('玻璃折射强度必须是 0 到 100 之间的整数');
  }
  if (config.sources.length > SITE_BACKGROUND_MAX_SOURCES) {
    return t('背景图片来源不能超过 20 个');
  }
  if (config.enabled && !config.sources.some((source) => source.enabled)) {
    return t('启用站点背景前请至少启用一个图片来源');
  }

  for (let index = 0; index < config.sources.length; index += 1) {
    const source = config.sources[index];
    if (
      !Number.isInteger(source.weight) ||
      source.weight < 1 ||
      source.weight > 100
    ) {
      return t('随机权重必须是 1 到 100 之间的整数');
    }
    if (!isAllowedSiteBackgroundURL(source.url)) {
      return t('第 {{index}} 个背景图片地址无效', { index: index + 1 });
    }
    if (
      source.type === SITE_BACKGROUND_SOURCE_TYPES.JSON_API &&
      source.json_path &&
      !JSON_PATH_PATTERN.test(source.json_path)
    ) {
      return t('第 {{index}} 个 JSON 路径无效', { index: index + 1 });
    }
  }
  return '';
};

const SiteBackgroundSetting = ({ value, onSaved }) => {
  const { t } = useTranslation();
  const [statusState, statusDispatch] = useContext(StatusContext);
  const [draft, setDraft] = useState(() => parseDraftConfig(value));
  const [saving, setSaving] = useState(false);
  const [previewLoading, setPreviewLoading] = useState(false);
  const [previewURL, setPreviewURL] = useState('');
  const [previewError, setPreviewError] = useState('');
  const sourceSignature = useMemo(
    () => JSON.stringify(draft.sources),
    [draft.sources],
  );

  useEffect(() => {
    setDraft(parseDraftConfig(value));
  }, [value]);

  useEffect(() => {
    let controller;
    let active = true;
    const usableSources = draft.sources.filter(
      (source) =>
        source.enabled && isAllowedSiteBackgroundURL(source.url),
    );

    if (usableSources.length === 0) {
      setPreviewLoading(false);
      setPreviewURL('');
      setPreviewError('');
      return undefined;
    }

    setPreviewLoading(true);
    setPreviewError('');
    const timer = window.setTimeout(() => {
      controller = new AbortController();
      resolveSiteBackground(usableSources, { signal: controller.signal })
        .then(({ url }) => {
          if (!active) return;
          setPreviewURL(url);
          setPreviewError('');
        })
        .catch((error) => {
          if (active && error?.name !== 'AbortError') {
            setPreviewURL('');
            setPreviewError(t('无法从当前来源加载背景图片'));
          }
        })
        .finally(() => {
          if (active) setPreviewLoading(false);
        });
    }, 600);

    return () => {
      active = false;
      window.clearTimeout(timer);
      controller?.abort();
    };
  }, [sourceSignature, t]);

  const updateDraft = (changes) => {
    setDraft((current) => ({ ...current, ...changes }));
  };

  const updateSource = (index, changes) => {
    setDraft((current) => ({
      ...current,
      sources: current.sources.map((source, sourceIndex) =>
        sourceIndex === index ? { ...source, ...changes } : source,
      ),
    }));
  };

  const addSource = () => {
    if (draft.sources.length >= SITE_BACKGROUND_MAX_SOURCES) return;
    updateDraft({
      sources: [
        ...draft.sources,
        {
          type: SITE_BACKGROUND_SOURCE_TYPES.IMAGE_URL,
          url: '',
          json_path: '',
          enabled: true,
          weight: 1,
        },
      ],
    });
  };

  const removeSource = (index) => {
    updateDraft({
      sources: draft.sources.filter((_, sourceIndex) => sourceIndex !== index),
    });
  };

  const saveConfig = async () => {
    const config = cleanDraftConfig(draft);
    const validationError = validateDraftConfig(config, t);
    if (validationError) {
      showError(validationError);
      return;
    }

    setSaving(true);
    try {
      const serialized = JSON.stringify(config);
      const response = await API.put('/api/option/', {
        key: SITE_BACKGROUND_OPTION_KEY,
        value: serialized,
      });
      const { success, message } = response.data;
      if (!success) {
        showError(message || t('站点背景设置保存失败'));
        return;
      }

      statusDispatch({
        type: 'set',
        payload: {
          ...(statusState?.status || {}),
          site_background: config,
        },
      });
      onSaved?.(serialized);
      showSuccess(t('站点背景设置已更新'));
    } catch (error) {
      console.error('站点背景设置保存失败:', error);
      showError(t('站点背景设置保存失败'));
    } finally {
      setSaving(false);
    }
  };

  const sourceTypeOptions = [
    {
      value: SITE_BACKGROUND_SOURCE_TYPES.IMAGE_URL,
      label: t('静态图片直链'),
    },
    {
      value: SITE_BACKGROUND_SOURCE_TYPES.IMAGE_API,
      label: t('直接图片接口'),
    },
    {
      value: SITE_BACKGROUND_SOURCE_TYPES.JSON_API,
      label: t('JSON 图片接口'),
    },
  ];
  const fitOptions = [
    { value: 'cover', label: t('铺满并裁切（Cover）') },
    { value: 'contain', label: t('完整显示（Contain）') },
    { value: 'fill', label: t('拉伸铺满（Fill）') },
  ];

  return (
    <div className='site-background-settings'>
      <Typography.Title heading={6}>{t('站点背景')}</Typography.Title>
      <Typography.Text type='tertiary'>
        {t('配置全站背景图片；随机来源会在每次完整刷新时重新选择。')}
      </Typography.Text>

      <div className='site-background-setting-grid'>
        <div className='site-background-setting-control'>
          <Typography.Text strong>{t('启用站点背景')}</Typography.Text>
          <Switch
            checked={draft.enabled}
            aria-label={t('启用站点背景')}
            onChange={(enabled) => updateDraft({ enabled })}
          />
        </div>
        <div className='site-background-setting-control'>
          <Typography.Text strong>{t('背景显示模式')}</Typography.Text>
          <Select
            value={draft.fit}
            aria-label={t('背景显示模式')}
            optionList={fitOptions}
            onChange={(fit) => updateDraft({ fit })}
            style={{ width: '100%' }}
          />
        </div>
        <div className='site-background-setting-control'>
          <Typography.Text strong>{t('背景遮罩强度')}</Typography.Text>
          <InputNumber
            value={draft.overlay_opacity}
            aria-label={t('背景遮罩强度')}
            min={0}
            max={80}
            step={1}
            suffix='%'
            onChange={(overlayOpacity) =>
              updateDraft({ overlay_opacity: overlayOpacity })
            }
            style={{ width: '100%' }}
          />
        </div>
        <div className='site-background-setting-control'>
          <Typography.Text strong>{t('启用液态玻璃')}</Typography.Text>
          <Switch
            checked={draft.glass_enabled}
            aria-label={t('启用液态玻璃')}
            onChange={(glassEnabled) =>
              updateDraft({ glass_enabled: glassEnabled })
            }
          />
        </div>
        <div className='site-background-setting-control'>
          <Typography.Text strong>{t('玻璃不透明度')}</Typography.Text>
          <InputNumber
            value={draft.glass_opacity}
            aria-label={t('玻璃不透明度')}
            min={0}
            max={100}
            step={1}
            suffix='%'
            disabled={!draft.glass_enabled}
            onChange={(glassOpacity) =>
              updateDraft({ glass_opacity: glassOpacity })
            }
            style={{ width: '100%' }}
          />
          <Typography.Text type='tertiary'>
            {t('数值越低越透明')}
          </Typography.Text>
        </div>
        <div className='site-background-setting-control'>
          <Typography.Text strong>{t('玻璃折射强度')}</Typography.Text>
          <InputNumber
            value={draft.glass_refraction}
            aria-label={t('玻璃折射强度')}
            min={0}
            max={100}
            step={1}
            suffix='%'
            disabled={!draft.glass_enabled}
            onChange={(glassRefraction) =>
              updateDraft({ glass_refraction: glassRefraction })
            }
            style={{ width: '100%' }}
          />
          <Typography.Text type='tertiary'>
            {t('边缘弯折背景的程度，0 为关闭折射；Safari 与 Firefox 暂不支持')}
          </Typography.Text>
        </div>
      </div>

      <div className='site-background-source-header'>
        <Typography.Text strong>{t('背景图片来源')}</Typography.Text>
        <Typography.Text type='tertiary'>{t('最多 20 个')}</Typography.Text>
      </div>

      <Space vertical align='start' spacing='medium' style={{ width: '100%' }}>
        {draft.sources.map((source, index) => (
          <Card
            key={index}
            className='site-background-source-card'
            bodyStyle={{ padding: 12 }}
          >
            <div className='site-background-source-row'>
              <div className='site-background-source-enabled'>
                <Typography.Text>{t('启用来源')}</Typography.Text>
                <Switch
                  checked={source.enabled}
                  aria-label={t('启用来源')}
                  onChange={(enabled) => updateSource(index, { enabled })}
                />
              </div>
              <Select
                value={source.type}
                optionList={sourceTypeOptions}
                onChange={(type) =>
                  updateSource(index, {
                    type,
                    ...(type !== SITE_BACKGROUND_SOURCE_TYPES.JSON_API
                      ? { json_path: '' }
                      : {}),
                  })
                }
                className='site-background-source-type'
              />
              <Input
                value={source.url}
                placeholder={
                  source.type === SITE_BACKGROUND_SOURCE_TYPES.IMAGE_URL
                    ? t('请输入图片直链或同源路径')
                    : t('请输入随机图片接口地址')
                }
                onChange={(url) => updateSource(index, { url })}
                className='site-background-source-url'
              />
              {source.type === SITE_BACKGROUND_SOURCE_TYPES.JSON_API && (
                <Input
                  value={source.json_path}
                  placeholder={t('JSON 路径，例如 image.compressed.url')}
                  onChange={(jsonPath) =>
                    updateSource(index, { json_path: jsonPath })
                  }
                  className='site-background-source-path'
                />
              )}
              <InputNumber
                value={source.weight}
                min={1}
                max={100}
                step={1}
                prefix={t('随机权重')}
                aria-label={t('随机权重')}
                disabled={!source.enabled}
                onChange={(weight) => updateSource(index, { weight })}
                className='site-background-source-weight'
              />
              <Button
                type='danger'
                theme='borderless'
                icon={<IconDelete />}
                aria-label={t('删除图片来源')}
                onClick={() => removeSource(index)}
              />
            </div>
          </Card>
        ))}
      </Space>

      <Button
        icon={<IconPlus />}
        theme='outline'
        onClick={addSource}
        disabled={draft.sources.length >= SITE_BACKGROUND_MAX_SOURCES}
        style={{ marginTop: 12 }}
      >
        {t('添加图片来源')}
      </Button>

      <Banner
        fullMode={false}
        type='warning'
        closeIcon={null}
        className='site-background-privacy-banner'
        description={t(
          'JSON 接口必须允许浏览器跨域访问。第三方图片或接口仍可看到访客 IP，请勿在地址中填写密钥。',
        )}
      />

      <div className='site-background-preview-section'>
        <Typography.Text strong>{t('自动预览')}</Typography.Text>
        <div className='site-background-preview'>
          <Spin spinning={previewLoading}>
            {previewURL ? (
              <>
                <img
                  src={previewURL}
                  alt={t('站点背景预览')}
                  referrerPolicy='no-referrer'
                  style={{ objectFit: draft.fit }}
                />
                <div
                  className='site-background-preview-overlay'
                  style={{
                    '--site-background-overlay-opacity':
                      Number(draft.overlay_opacity) / 100,
                  }}
                />
                {draft.glass_enabled ? (
                  <>
                    <SiteBackgroundGlassFilter
                      refraction={draft.glass_refraction}
                      filterId={SITE_BACKGROUND_GLASS_PREVIEW_FILTER_ID}
                    />
                    <div
                      className='site-background-preview-glass'
                      style={{
                        '--site-background-glass-opacity': `${Number(
                          draft.glass_opacity,
                        )}%`,
                        '--site-background-glass-veil': `${resolveSiteBackgroundGlassVeil(
                          draft.glass_opacity,
                        )}%`,
                        ...(Number(draft.glass_refraction) > 0
                          ? {
                              '--site-background-glass-refract-light': `url(#${SITE_BACKGROUND_GLASS_PREVIEW_FILTER_ID})`,
                              '--site-background-glass-refract-dark': `url(#${darkVariantFilterId(
                                SITE_BACKGROUND_GLASS_PREVIEW_FILTER_ID,
                              )})`,
                            }
                          : {}),
                      }}
                    >
                      {t('液态玻璃预览')}
                    </div>
                  </>
                ) : null}
              </>
            ) : (
              <div className='site-background-preview-empty'>
                {previewError || t('添加有效来源后将在这里自动预览')}
              </div>
            )}
          </Spin>
        </div>
      </div>

      <Button type='primary' loading={saving} onClick={saveConfig}>
        {t('保存站点背景设置')}
      </Button>
    </div>
  );
};

export default SiteBackgroundSetting;
