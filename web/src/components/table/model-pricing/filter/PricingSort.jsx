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
import { Button, Divider, Select, Typography } from '@douyinfe/semi-ui';
import { getPricingSortOptions } from '../../../../helpers/modelPricing';

const PricingSort = ({
  sortBy,
  setSortBy,
  sortDirection,
  setSortDirection,
  loading = false,
  t,
}) => {
  const labelId = React.useId();
  const isDefault = sortBy === 'default';
  const isName = sortBy === 'model_name';
  const directions = [
    { value: 'asc', label: isName ? 'A–Z' : t('从低到高') },
    { value: 'desc', label: isName ? 'Z–A' : t('从高到低') },
  ];

  return (
    <div className='mb-8'>
      <Divider margin='12px' align='left'>
        {t('排序')}
      </Divider>
      <span id={labelId} className='sr-only'>
        {t('排序依据')}
      </span>
      <Select
        aria-labelledby={labelId}
        value={sortBy}
        onChange={setSortBy}
        optionList={getPricingSortOptions(t)}
        disabled={loading}
        style={{ width: '100%' }}
      />
      {!isDefault && (
        <div
          className='flex gap-2 mt-2'
          role='group'
          aria-label={t('排序方向')}
        >
          {directions.map(({ value, label }) => (
            <Button
              key={value}
              theme={sortDirection === value ? 'light' : 'outline'}
              type={sortDirection === value ? 'primary' : 'tertiary'}
              aria-pressed={sortDirection === value}
              disabled={loading}
              className='flex-1 min-w-0'
              onClick={() => setSortDirection(value)}
            >
              {label}
            </Button>
          ))}
        </div>
      )}
      {!isDefault && !isName && (
        <div className='mt-2'>
          <Typography.Text type='tertiary' size='small'>
            {t('按当前分组价格排序，无对应固定价格的模型置后')}
          </Typography.Text>
        </div>
      )}
    </div>
  );
};

export default PricingSort;
