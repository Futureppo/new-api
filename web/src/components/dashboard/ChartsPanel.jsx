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
import { Card, Tabs, TabPane } from '@douyinfe/semi-ui';
import { PieChart } from 'lucide-react';
import { VChart } from '@visactor/react-vchart';

const ChartsPanel = ({
  activeChartTab,
  setActiveChartTab,
  spec_line,
  spec_model_line,
  spec_pie,
  spec_rank_bar,
  spec_user_rank,
  spec_user_trend,
  isAdminUser,
  CARD_PROPS,
  CHART_CONFIG,
  FLEX_CENTER_GAP2,
  hasApiInfoPanel,
  chartTabs,
  title = '模型数据分析',
  t,
}) => {
  const tabs = chartTabs || [
    { key: '1', label: '消耗分布', spec: spec_line },
    { key: '2', label: '调用趋势', spec: spec_model_line },
    { key: '3', label: '调用次数分布', spec: spec_pie },
    { key: '4', label: '调用次数排行', spec: spec_rank_bar },
    ...(isAdminUser
      ? [
          { key: '5', label: '用户消耗排行', spec: spec_user_rank },
          { key: '6', label: '用户消耗趋势', spec: spec_user_trend },
        ]
      : []),
  ];
  const activeChart = tabs.find((tab) => tab.key === activeChartTab);
  return (
    <Card
      {...CARD_PROPS}
      className={`!rounded-2xl ${hasApiInfoPanel ? 'lg:col-span-3' : ''}`}
      title={
        <div className='flex flex-col lg:flex-row lg:items-center lg:justify-between w-full gap-3'>
          <div className={FLEX_CENTER_GAP2}>
            <PieChart size={16} />
            {t(title)}
          </div>
          <Tabs
            type='slash'
            activeKey={activeChartTab}
            onChange={setActiveChartTab}
          >
            {tabs.map((tab) => (
              <TabPane
                key={tab.key}
                tab={<span>{t(tab.label)}</span>}
                itemKey={tab.key}
              />
            ))}
          </Tabs>
        </div>
      }
      bodyStyle={{ padding: 0 }}
    >
      <div className='h-96 p-2'>
        {activeChart && (
          <VChart
            key={activeChart.key}
            spec={activeChart.spec}
            option={CHART_CONFIG}
          />
        )}
      </div>
    </Card>
  );
};

export default ChartsPanel;
