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

const NodeLocIcon = ({ style, ...props }) => (
  <svg
    viewBox='0 0 20 20'
    width='1em'
    height='1em'
    aria-hidden='true'
    style={{ stroke: 'currentColor', ...style }}
    {...props}
  >
    <circle cx='10' cy='10' r='9' fill='none' />
    <path
      d={'M6 14V6L14 14V6'}
      fill='none'
      strokeWidth='1.8'
      strokeLinejoin={'round'}
    />
  </svg>
);

export default NodeLocIcon;
