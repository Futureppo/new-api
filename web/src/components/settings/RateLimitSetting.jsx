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

import React, { useEffect, useState } from 'react';
import { Card, Spin } from '@douyinfe/semi-ui';

import { API, showError, toBoolean } from '../../helpers';
import { useTranslation } from 'react-i18next';
import RequestRateLimit, {
  ProbeGuardRateLimit,
} from '../../pages/Setting/RateLimit/SettingsRequestRateLimit';

const RateLimitSetting = () => {
  const { t } = useTranslation();
  let [inputs, setInputs] = useState({
    ModelRequestRateLimitEnabled: false,
    ModelRequestRateLimitCount: 0,
    ModelRequestRateLimitSuccessCount: 1000,
    ModelRequestRateLimitDurationMinutes: 1,
    ModelRequestConcurrencyLimit: 2,
    ModelRequestRateLimitGroup: '',
    'probe_guard_setting.enabled': false,
    'probe_guard_setting.dry_run': true,
    'probe_guard_setting.window_seconds': 60,
    'probe_guard_setting.distinct_model_count': 5,
    'probe_guard_setting.first_ip_ban_minutes': 10,
    'probe_guard_setting.second_ip_ban_minutes': 60,
    'probe_guard_setting.permanent_offense_count': 3,
    'probe_guard_setting.offense_dedupe_seconds': 60,
    'probe_guard_setting.max_ips_per_offense': 32,
    'probe_guard_setting.whitelist_user_ids': '',
  });

  let [loading, setLoading] = useState(false);

  const getOptions = async () => {
    const res = await API.get('/api/option/');
    const { success, message, data } = res.data;
    if (success) {
      let newInputs = {};
      data.forEach((item) => {
        if (item.key === 'ModelRequestRateLimitGroup') {
          item.value = JSON.stringify(JSON.parse(item.value), null, 2);
        }

        if (
          item.key.endsWith('Enabled') ||
          item.key === 'probe_guard_setting.enabled' ||
          item.key === 'probe_guard_setting.dry_run'
        ) {
          newInputs[item.key] = toBoolean(item.value);
        } else {
          newInputs[item.key] = item.value;
        }
      });

      setInputs(newInputs);
    } else {
      showError(message);
    }
  };
  async function onRefresh() {
    try {
      setLoading(true);
      await getOptions();
      // showSuccess('刷新成功');
    } catch (error) {
      showError('刷新失败');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    onRefresh();
  }, []);

  return (
    <>
      <Spin spinning={loading} size='large'>
        {/* AI请求速率限制 */}
        <Card style={{ marginTop: '10px' }}>
          <RequestRateLimit options={inputs} refresh={onRefresh} />
        </Card>
        <Card style={{ marginTop: '10px' }}>
          <ProbeGuardRateLimit options={inputs} refresh={onRefresh} />
        </Card>
      </Spin>
    </>
  );
};

export default RateLimitSetting;
