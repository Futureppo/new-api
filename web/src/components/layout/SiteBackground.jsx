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
  normalizeSiteBackgroundConfig,
  resolveSiteBackgroundAsset,
} from '../../services/siteBackground';
import SiteBackgroundGlassFilter from './SiteBackgroundGlassFilter';
import SiteBackgroundGlassCanvas from './SiteBackgroundGlassCanvas';

const SiteBackground = ({ config, onAssetChange }) => {
  const normalizedConfig = useMemo(
    () => normalizeSiteBackgroundConfig(config),
    [config],
  );
  const sourceSignature = useMemo(
    () => JSON.stringify(normalizedConfig.sources),
    [normalizedConfig.sources],
  );
  const [imageAsset, setImageAsset] = useState(null);
  const [webglFailed, setWebglFailed] = useState(false);
  const imageURL = imageAsset?.url || '';

  // 管理员把渲染器切回 webgl（或换了配置）时给它重试机会
  useEffect(() => {
    setWebglFailed(false);
  }, [normalizedConfig.glass_renderer]);

  useEffect(() => {
    if (!normalizedConfig.enabled) {
      setImageAsset(null);
      return undefined;
    }

    let active = true;
    setImageAsset(null);
    const controller = new AbortController();
    resolveSiteBackgroundAsset(normalizedConfig.sources, {
      signal: controller.signal,
    })
      .then((asset) => {
        if (active) {
          setImageAsset(asset);
        } else {
          URL.revokeObjectURL(asset.url);
        }
      })
      .catch((error) => {
        if (active && error?.name !== 'AbortError') {
          console.warn('站点背景加载失败:', error);
          setImageAsset(null);
        }
      });

    return () => {
      active = false;
      controller.abort();
    };
  }, [normalizedConfig.enabled, sourceSignature]);

  useEffect(() => {
    onAssetChange?.(normalizedConfig.enabled ? imageAsset : null);
  }, [imageAsset, normalizedConfig.enabled, onAssetChange]);

  useEffect(
    () => () => {
      if (imageAsset?.url) URL.revokeObjectURL(imageAsset.url);
    },
    [imageAsset],
  );

  if (!normalizedConfig.enabled) return null;

  const webglActive =
    normalizedConfig.glass_enabled &&
    normalizedConfig.glass_renderer === 'webgl' &&
    !webglFailed;

  // WebGL 路径由画布负责背景、遮罩与玻璃磨砂；CSS 路径维持原样。
  // 渲染器运行中失败（上下文丢失、图源跨域受限）会切回 CSS 路径，此时
  // 不再补挂 SVG 折射滤镜——折射本就是可选增强，回退以稳为先。
  return (
    <div className='site-background-layer' aria-hidden='true'>
      {normalizedConfig.glass_enabled && !webglActive && (
        <SiteBackgroundGlassFilter
          refraction={normalizedConfig.glass_refraction}
        />
      )}
      {webglActive ? (
        <SiteBackgroundGlassCanvas
          config={normalizedConfig}
          imageURL={imageURL}
          onFallback={(reason) => {
            console.warn('站点背景 WebGL 渲染不可用，退回 CSS:', reason);
            setWebglFailed(true);
          }}
        />
      ) : (
        <>
          {imageURL && (
            <img
              className='site-background-image site-background-image-loaded'
              src={imageURL}
              alt=''
              referrerPolicy='no-referrer'
              style={{ objectFit: normalizedConfig.fit }}
            />
          )}
          <div
            className='site-background-overlay'
            style={{
              '--site-background-overlay-opacity':
                normalizedConfig.overlay_opacity / 100,
            }}
          />
        </>
      )}
    </div>
  );
};

export default SiteBackground;
