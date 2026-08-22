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

import React, { useEffect, useRef } from 'react';
import { createSiteBackgroundGlassRenderer } from './siteBackgroundGlassRenderer';

/*
 * WebGL 玻璃渲染路径的挂载组件。挂载期间给 body 加
 * site-background-glass-webgl，CSS 据此关掉 DOM 侧的 backdrop-filter；
 * 渲染器任何一步失败（不支持 WebGL、上下文丢失、图源跨域受限）都会走
 * onFallback，由 SiteBackground 切回 CSS 路径，卸载时类名一并清掉。
 */
const SiteBackgroundGlassCanvas = ({ config, imageURL, onFallback }) => {
  const canvasRef = useRef(null);
  const rendererRef = useRef(null);
  const fallbackRef = useRef(onFallback);
  fallbackRef.current = onFallback;

  useEffect(() => {
    const renderer = createSiteBackgroundGlassRenderer({
      canvas: canvasRef.current,
      onFallback: (reason) => fallbackRef.current?.(reason),
    });
    if (!renderer) {
      fallbackRef.current?.('unsupported');
      return undefined;
    }
    rendererRef.current = renderer;
    document.body.classList.add('site-background-glass-webgl');
    return () => {
      document.body.classList.remove('site-background-glass-webgl');
      renderer.destroy();
      rendererRef.current = null;
    };
  }, []);

  useEffect(() => {
    rendererRef.current?.setOptions({
      fit: config.fit,
      overlayOpacity: config.overlay_opacity / 100,
      refraction: config.glass_refraction,
      clarity: config.glass_edge_clarity / 100,
      // 滑杆 0-100 映射到 R/B 偏移比例 0-0.6；40 档对应边缘约 ±24% 位移差
      dispersion: (config.glass_dispersion / 100) * 0.6,
      edgeLight: config.glass_edge_light / 100,
    });
  }, [
    config.fit,
    config.overlay_opacity,
    config.glass_refraction,
    config.glass_edge_clarity,
    config.glass_dispersion,
    config.glass_edge_light,
  ]);

  useEffect(() => {
    if (imageURL) rendererRef.current?.setImage(imageURL);
  }, [imageURL]);

  return <canvas ref={canvasRef} className='site-background-glass-canvas' />;
};

export default SiteBackgroundGlassCanvas;
