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

import React, { useMemo } from 'react';

export const SITE_BACKGROUND_GLASS_FILTER_ID = 'site-background-glass-refract';
// 设置页的预览与站点本体会同时挂载滤镜，必须用不同的 id，否则 url(#id)
// 只会命中文档里第一个同名 filter，预览就永远显示已保存值而不是草稿值。
export const SITE_BACKGROUND_GLASS_PREVIEW_FILTER_ID =
  'site-background-glass-refract-preview';

/*
 * 液态玻璃的折射层。
 *
 * backdrop-filter: blur() 只是把背景漫射掉，那是磨砂塑料的物理行为；玻璃真正
 * 的辨识特征是折射——边缘把背景压缩、弯折，并且因为不同波长折射率不同而产生
 * 色散。这里用 feDisplacementMap 做位移，用 R/G/B 三条位移量略有差异的链路再
 * 合成来得到色散。
 *
 * 位移贴图的 R 通道编码横向位移、G 通道编码纵向位移，128 为中性（不位移）。
 * 两个方向合并进同一张图是关键的性能决策：如果拆成两张图、串联两次
 * feDisplacementMap，第二次要吃第一次的输出，会强制产生中间缓冲并且每帧多解一
 * 张贴图，实测在卡片密集页会把帧率打到个位数。合并之后每个通道只需一次位移。
 *
 * 渐变不是线性的，而是按透镜曲线在最外缘急弯、向内快速衰减到中性，这样折射集
 * 中在边缘，中心保持通透。
 */

// 透镜曲线采样点：[到边缘的相对距离, 向中性值收敛的比例]
const LENS_PROFILE = [
  [0, 0],
  [0.02, 0.1],
  [0.05, 0.26],
  [0.1, 0.5],
  [0.18, 0.72],
  [0.3, 0.87],
  [0.5, 0.96],
  [1, 1],
];

const NEUTRAL = 128;

const buildGradientStops = (band, horizontal) => {
  const edge = band / 100;
  const stops = [];

  LENS_PROFILE.forEach(([distance, converge]) => {
    stops.push([distance * edge, Math.round(NEUTRAL * converge)]);
  });
  LENS_PROFILE.slice()
    .reverse()
    .forEach(([distance, converge]) => {
      stops.push([
        1 - distance * edge,
        Math.round(255 - (255 - NEUTRAL) * converge),
      ]);
    });

  return stops
    .map(([offset, value]) => {
      // 横向渐变只写 R（外加恒定的 B），纵向渐变只写 G，下面用 screen 叠加时
      // 各通道互不干扰：screen 为 1-(1-a)(1-b)，与 0 相叠等于保留原值。
      const color = horizontal
        ? `rgb(${value},0,${NEUTRAL})`
        : `rgb(0,${value},0)`;
      return `<stop offset="${offset.toFixed(4)}" stop-color="${color}"/>`;
    })
    .join('');
};

// 一张贴图同时编码两个方向：R = 横向位移，G = 纵向位移，B 恒为中性。
const buildLensMap = (band) => {
  const svg =
    '<svg xmlns="http://www.w3.org/2000/svg" width="400" height="400" preserveAspectRatio="none">' +
    '<defs>' +
    `<linearGradient id="h" x1="0" y1="0" x2="1" y2="0">${buildGradientStops(band, true)}</linearGradient>` +
    `<linearGradient id="v" x1="0" y1="0" x2="0" y2="1">${buildGradientStops(band, false)}</linearGradient>` +
    '</defs>' +
    '<rect width="400" height="400" fill="url(#h)"/>' +
    '<rect width="400" height="400" fill="url(#v)" style="mix-blend-mode:screen"/>' +
    '</svg>';

  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
};

// 0-100 的折射强度映射到实际光学参数
const resolveOptics = (refraction) => {
  const ratio = Math.min(100, Math.max(0, Number(refraction) || 0)) / 100;
  return {
    // 边缘折射带占卡片尺寸的比例
    band: 20 + ratio * 16,
    // 位移量（相对包围盒）
    scale: ratio * 0.6,
    // 色散：R/G/B 三条链路的位移量差异
    dispersion: ratio * 0.26,
  };
};

const SiteBackgroundGlassFilter = ({
  refraction,
  filterId = SITE_BACKGROUND_GLASS_FILTER_ID,
}) => {
  const optics = useMemo(() => resolveOptics(refraction), [refraction]);
  const lensMap = useMemo(() => buildLensMap(optics.band), [optics.band]);

  if (optics.scale <= 0) return null;

  const channels = [
    { key: 'r', scale: optics.scale * (1 + optics.dispersion) },
    { key: 'g', scale: optics.scale },
    { key: 'b', scale: optics.scale * (1 - optics.dispersion) },
  ];

  return (
    <svg
      className='site-background-glass-filter'
      width='0'
      height='0'
      aria-hidden='true'
      focusable='false'
    >
      <defs>
        <filter
          id={filterId}
          filterUnits='objectBoundingBox'
          primitiveUnits='objectBoundingBox'
          x='0'
          y='0'
          width='1'
          height='1'
          colorInterpolationFilters='sRGB'
        >
          <feImage
            href={lensMap}
            result='lensMap'
            preserveAspectRatio='none'
            x='0'
            y='0'
            width='1'
            height='1'
          />

          {channels.map(({ key, scale }) => (
            <feDisplacementMap
              key={key}
              in='SourceGraphic'
              in2='lensMap'
              scale={scale}
              xChannelSelector='R'
              yChannelSelector='G'
              result={`${key}Displaced`}
            />
          ))}

          {/* 各取一个通道再叠回去，位移量的差异就成了色散 */}
          <feColorMatrix
            in='rDisplaced'
            values='1 0 0 0 0  0 0 0 0 0  0 0 0 0 0  0 0 0 1 0'
            result='rOnly'
          />
          <feColorMatrix
            in='gDisplaced'
            values='0 0 0 0 0  0 1 0 0 0  0 0 0 0 0  0 0 0 1 0'
            result='gOnly'
          />
          <feColorMatrix
            in='bDisplaced'
            values='0 0 0 0 0  0 0 0 0 0  0 0 1 0 0  0 0 0 1 0'
            result='bOnly'
          />
          <feBlend in='rOnly' in2='gOnly' mode='screen' result='rgMerged' />
          <feBlend in='rgMerged' in2='bOnly' mode='screen' />
        </filter>
      </defs>
    </svg>
  );
};

export default SiteBackgroundGlassFilter;
