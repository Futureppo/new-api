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
import { Card, Table, Empty } from '@douyinfe/semi-ui';
import {
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';
import { getPricingTableColumns } from './PricingTableColumns';
import { useIsMobile } from '../../../../../hooks/common/useIsMobile';

const PricingTable = ({
  filteredModels,
  loading,
  rowSelection,
  pageSize,
  setPageSize,
  currentPage,
  setCurrentPage,
  sortBy,
  tableSortOrder,
  setTableSortOrder,
  selectedGroup,
  groupRatio,
  copyText,
  setModalImageUrl,
  setIsModalOpenurl,
  currency,
  siteDisplayType,
  tokenUnit,
  displayPrice,
  showRatio,
  compactMode = false,
  openModelDetail,
  t,
}) => {
  const isMobile = useIsMobile();
  const columns = useMemo(() => {
    return getPricingTableColumns({
      t,
      isMobile,
      selectedGroup,
      groupRatio,
      copyText,
      setModalImageUrl,
      setIsModalOpenurl,
      currency,
      siteDisplayType,
      tokenUnit,
      displayPrice,
      showRatio,
    });
  }, [
    t,
    isMobile,
    selectedGroup,
    groupRatio,
    copyText,
    setModalImageUrl,
    setIsModalOpenurl,
    currency,
    siteDisplayType,
    tokenUnit,
    displayPrice,
    showRatio,
  ]);

  // 搜索已由共享 hook 完成；只有默认排序允许表格列单独排序。
  const processedColumns = useMemo(() => {
    const cols = columns.map((column) => {
      if (column.dataIndex === 'quota_type') {
        return {
          ...column,
          sorter: sortBy === 'default',
          sortOrder: sortBy === 'default' ? tableSortOrder : false,
        };
      }
      return column;
    });

    // Remove fixed property when in compact mode (mobile view)
    if (compactMode) {
      return cols.map(({ fixed, ...rest }) => rest);
    }
    return cols;
  }, [columns, sortBy, tableSortOrder, compactMode]);

  // Semi treats controlled pagination as remote data: slice only after sorting
  // the complete result, including the legacy quota-type column order.
  const paginatedModels = useMemo(() => {
    const models =
      sortBy === 'default' && tableSortOrder
        ? [...filteredModels].sort(
            (a, b) =>
              (a.quota_type - b.quota_type) *
              (tableSortOrder === 'descend' ? -1 : 1),
          )
        : filteredModels;
    const start = (currentPage - 1) * pageSize;
    return models.slice(start, start + pageSize);
  }, [filteredModels, sortBy, tableSortOrder, currentPage, pageSize]);

  const ModelTable = useMemo(
    () => (
      <Card className='!rounded-xl overflow-hidden' bordered={false}>
        <Table
          columns={processedColumns}
          dataSource={paginatedModels}
          loading={loading}
          rowSelection={rowSelection}
          onChange={({ sorter, extra }) => {
            if (sortBy === 'default' && extra?.changeType === 'sorter') {
              setTableSortOrder(sorter?.sortOrder || false);
              setCurrentPage(1);
            }
          }}
          scroll={compactMode ? undefined : { x: 'max-content' }}
          onRow={(record) => ({
            onClick: () => openModelDetail && openModelDetail(record),
            style: { cursor: 'pointer' },
          })}
          empty={
            <Empty
              image={
                <IllustrationNoResult style={{ width: 150, height: 150 }} />
              }
              darkModeImage={
                <IllustrationNoResultDark style={{ width: 150, height: 150 }} />
              }
              description={t('搜索无结果')}
              style={{ padding: 30 }}
            />
          }
          pagination={{
            currentPage,
            total: filteredModels.length,
            pageSize: pageSize,
            showSizeChanger: true,
            pageSizeOptions: [10, 20, 50, 100],
            onPageChange: setCurrentPage,
            onPageSizeChange: (size) => {
              setPageSize(size);
              setCurrentPage(1);
            },
          }}
        />
      </Card>
    ),
    [
      filteredModels,
      paginatedModels,
      loading,
      processedColumns,
      rowSelection,
      pageSize,
      setPageSize,
      currentPage,
      setCurrentPage,
      sortBy,
      setTableSortOrder,
      openModelDetail,
      t,
      compactMode,
    ],
  );

  return ModelTable;
};

export default PricingTable;
