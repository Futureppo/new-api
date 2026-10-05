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
import agnes from '../assets/model-brands/agnesai.svg';
import apodex from '../assets/model-brands/apodex.png';
import dots from '../assets/model-brands/dotsstudio.svg';
import kilo from '../assets/model-brands/kilocode.svg';
import longcat from '../assets/model-brands/longcat.svg';
import poolside from '../assets/model-brands/poolside.svg';
import thinkingMachines from '../assets/model-brands/thinkingmachines.png';
import typeSafe from '../assets/model-brands/typesafe.png';
import mimo from '../assets/model-brands/xiaomimimo-custom.png';

// Bundled brand assets supplement the installed LobeHub icon set. SVG masks
// inherit the current text color in both themes; raster logos retain branding.
function brandIcon(name, src, monochrome = true) {
  function BrandIcon({ size = 14, className, style, title }) {
    const label = title || name;
    const box = {
      display: 'inline-block',
      width: size,
      height: size,
      flexShrink: 0,
      verticalAlign: 'middle',
    };
    return monochrome ? (
      <span
        role='img'
        aria-label={label}
        title={label}
        className={className}
        style={{
          ...box,
          backgroundColor: 'currentColor',
          mask: `url("${src}") center / contain no-repeat`,
          WebkitMask: `url("${src}") center / contain no-repeat`,
          ...style,
        }}
      />
    ) : (
      <img
        src={src}
        alt={label}
        title={label}
        className={className}
        style={{ ...box, objectFit: 'contain', ...style }}
      />
    );
  }
  BrandIcon.displayName = name;
  return BrandIcon;
}

export const modelBrandIcons = {
  AgnesAI: brandIcon('Agnes AI', agnes),
  Apodex: brandIcon('Apodex AI', apodex, false),
  DotsStudio: brandIcon('Dots Studio', dots),
  KiloCode: brandIcon('Kilo', kilo),
  LongCat: brandIcon('LongCat', longcat),
  Poolside: brandIcon('Poolside', poolside),
  ThinkingMachines: brandIcon('Thinking Machines Lab', thinkingMachines, false),
  TypeSafe: brandIcon('TypeSafe AI', typeSafe, false),
  XiaomiMiMo: brandIcon('Xiaomi MiMo', mimo, false),
};
