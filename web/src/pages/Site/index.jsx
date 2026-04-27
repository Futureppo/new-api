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

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Banner,
  Button,
  Card,
  Col,
  Form,
  InputNumber,
  Modal,
  Radio,
  RadioGroup,
  Row,
  Select,
  Spin,
  Typography,
} from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import {
  API,
  getCurrencyConfig,
  renderQuota,
  showError,
  showSuccess,
  showWarning,
} from '../../helpers';
import { displayAmountToQuota } from '../../helpers/quota';

const { Text } = Typography;

const Site = () => {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(true);
  const [previewLoading, setPreviewLoading] = useState(false);
  const [balancePreviewLoading, setBalancePreviewLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [balanceSubmitting, setBalanceSubmitting] = useState(false);
  const [sourceGroups, setSourceGroups] = useState([]);
  const [targetGroups, setTargetGroups] = useState([]);
  const [sourceGroup, setSourceGroup] = useState('');
  const [targetGroup, setTargetGroup] = useState('');
  const [previewCount, setPreviewCount] = useState(null);
  const [balanceGroup, setBalanceGroup] = useState('');
  const [balanceMode, setBalanceMode] = useState('add');
  const [balanceAmount, setBalanceAmount] = useState(null);
  const [balancePreview, setBalancePreview] = useState(null);

  const sourceOptions = useMemo(
    () =>
      sourceGroups.map((item) => ({
        label: `${item.group} (${item.count})`,
        value: item.group,
      })),
    [sourceGroups],
  );

  const targetOptions = useMemo(
    () =>
      targetGroups.map((group) => ({
        label: group,
        value: group,
      })),
    [targetGroups],
  );

  const balanceModeLabels = useMemo(
    () => ({
      add: t('增加余额'),
      subtract: t('减少余额'),
      override: t('覆盖余额'),
    }),
    [t],
  );

  const validBalanceQuota = useMemo(() => {
    const value = Number(balanceAmount);
    if (!Number.isFinite(value) || value <= 0) return 0;
    const quota = displayAmountToQuota(value);
    return Number.isInteger(quota) && quota > 0 ? quota : 0;
  }, [balanceAmount]);

  const formatQuotaValue = (quota) => `${renderQuota(quota)} (${quota})`;

  const loadOptions = useCallback(async () => {
    setLoading(true);
    try {
      const res = await API.get('/api/site/group-transfer/options');
      const { success, message, data } = res.data;
      if (!success) {
        showError(message);
        return;
      }

      const nextSourceGroups = Array.isArray(data?.source_groups)
        ? data.source_groups
        : [];
      const nextTargetGroups = Array.isArray(data?.target_groups)
        ? data.target_groups
        : [];
      setSourceGroups(nextSourceGroups);
      setTargetGroups(nextTargetGroups);
      setSourceGroup((prev) =>
        nextSourceGroups.some((item) => item.group === prev) ? prev : '',
      );
      setBalanceGroup((prev) =>
        nextSourceGroups.some((item) => item.group === prev) ? prev : '',
      );
      setTargetGroup((prev) => (nextTargetGroups.includes(prev) ? prev : ''));
    } catch (error) {
      showError(error);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadOptions();
  }, [loadOptions]);

  useEffect(() => {
    let ignore = false;

    async function loadPreview() {
      if (!sourceGroup || !targetGroup || sourceGroup === targetGroup) {
        setPreviewCount(null);
        return;
      }

      setPreviewLoading(true);
      try {
        const res = await API.get('/api/site/group-transfer/preview', {
          params: {
            source_group: sourceGroup,
            target_group: targetGroup,
          },
          disableDuplicate: true,
        });
        const { success, message, data } = res.data;
        if (ignore) return;
        if (!success) {
          setPreviewCount(null);
          showError(message);
          return;
        }
        setPreviewCount(Number(data?.affected ?? 0));
      } catch (error) {
        if (!ignore) {
          setPreviewCount(null);
          showError(error);
        }
      } finally {
        if (!ignore) {
          setPreviewLoading(false);
        }
      }
    }

    loadPreview();

    return () => {
      ignore = true;
    };
  }, [sourceGroup, targetGroup]);

  useEffect(() => {
    let ignore = false;

    async function loadBalancePreview() {
      if (!balanceGroup || !balanceMode || !validBalanceQuota) {
        setBalancePreview(null);
        return;
      }

      setBalancePreviewLoading(true);
      try {
        const res = await API.get('/api/site/group-balance/preview', {
          params: {
            group: balanceGroup,
            mode: balanceMode,
            quota: validBalanceQuota,
          },
          disableDuplicate: true,
        });
        const { success, message, data } = res.data;
        if (ignore) return;
        if (!success) {
          setBalancePreview(null);
          showError(message);
          return;
        }
        setBalancePreview(data);
      } catch (error) {
        if (!ignore) {
          setBalancePreview(null);
          showError(error);
        }
      } finally {
        if (!ignore) {
          setBalancePreviewLoading(false);
        }
      }
    }

    loadBalancePreview();

    return () => {
      ignore = true;
    };
  }, [balanceGroup, balanceMode, validBalanceQuota]);

  const validationMessage = useMemo(() => {
    if (!sourceGroup || !targetGroup) {
      return t('请选择源分组和目标分组');
    }
    if (sourceGroup === targetGroup) {
      return t('源分组和目标分组不能相同');
    }
    if (previewCount === 0) {
      return t('预计影响人数为 0，无法执行迁移');
    }
    return '';
  }, [previewCount, sourceGroup, targetGroup, t]);

  const canSubmit =
    sourceGroup &&
    targetGroup &&
    sourceGroup !== targetGroup &&
    previewCount > 0 &&
    !previewLoading &&
    !submitting;

  const balanceValidationMessage = useMemo(() => {
    if (!balanceGroup) {
      return t('请选择分组');
    }
    if (!validBalanceQuota) {
      return t('请输入大于 0 的金额');
    }
    if (balancePreview?.affected === 0) {
      return t('预计影响人数为 0，无法执行修改');
    }
    return '';
  }, [balanceGroup, balancePreview?.affected, t, validBalanceQuota]);

  const canSubmitBalance =
    balanceGroup &&
    validBalanceQuota > 0 &&
    balancePreview?.affected > 0 &&
    !balancePreviewLoading &&
    !balanceSubmitting;

  const executeTransfer = () => {
    if (!sourceGroup || !targetGroup) {
      showWarning(t('请选择源分组和目标分组'));
      return;
    }
    if (sourceGroup === targetGroup) {
      showWarning(t('源分组和目标分组不能相同'));
      return;
    }
    if (!previewCount || previewCount <= 0) {
      showWarning(t('预计影响人数为 0，无法执行迁移'));
      return;
    }

    Modal.confirm({
      title: t('确认分组调整'),
      content: (
        <div style={{ lineHeight: 1.8 }}>
          <div>
            {t('源分组')}：<Text strong>{sourceGroup}</Text>
          </div>
          <div>
            {t('目标分组')}：<Text strong>{targetGroup}</Text>
          </div>
          <div>
            {t('预计影响人数')}：<Text strong>{previewCount}</Text>
          </div>
        </div>
      ),
      okText: t('确认执行'),
      cancelText: t('取消'),
      okButtonProps: { type: 'danger', loading: submitting },
      onOk: async () => {
        setSubmitting(true);
        try {
          const res = await API.post('/api/site/group-transfer', {
            source_group: sourceGroup,
            target_group: targetGroup,
          });
          const { success, message, data } = res.data;
          if (!success) {
            showError(message);
            return;
          }

          const affected = Number(data?.affected ?? 0);
          showSuccess(
            t('分组调整完成，共迁移 {{count}} 个用户', { count: affected }),
          );
          await loadOptions();
          setPreviewCount(affected > 0 ? 0 : affected);
        } catch (error) {
          showError(error);
        } finally {
          setSubmitting(false);
        }
      },
    });
  };

  const executeBalanceUpdate = () => {
    if (!balanceGroup) {
      showWarning(t('请选择分组'));
      return;
    }
    if (!validBalanceQuota) {
      showWarning(t('请输入大于 0 的金额'));
      return;
    }
    if (!balancePreview || balancePreview.affected <= 0) {
      showWarning(t('预计影响人数为 0，无法执行修改'));
      return;
    }

    Modal.confirm({
      title: t('确认批量修改余额'),
      content: (
        <div style={{ lineHeight: 1.8 }}>
          <div>
            {t('分组')}：<Text strong>{balanceGroup}</Text>
          </div>
          <div>
            {t('操作')}：<Text strong>{balanceModeLabels[balanceMode]}</Text>
          </div>
          <div>
            {t('金额')}：
            <Text strong>{formatQuotaValue(validBalanceQuota)}</Text>
          </div>
          <div>
            {t('预计影响人数')}：<Text strong>{balancePreview.affected}</Text>
          </div>
          <div>
            {t('预计总变化')}：
            <Text strong>{formatQuotaValue(balancePreview.total_delta)}</Text>
          </div>
        </div>
      ),
      okText: t('确认执行'),
      cancelText: t('取消'),
      okButtonProps: { type: 'danger', loading: balanceSubmitting },
      onOk: async () => {
        setBalanceSubmitting(true);
        try {
          const res = await API.post('/api/site/group-balance', {
            group: balanceGroup,
            mode: balanceMode,
            quota: validBalanceQuota,
          });
          const { success, message, data } = res.data;
          if (!success) {
            showError(message);
            return;
          }

          setBalancePreview(data);
          showSuccess(
            t('批量修改余额完成，影响 {{count}} 个用户，总变化 {{delta}}', {
              count: data?.affected ?? 0,
              delta: renderQuota(data?.total_delta ?? 0),
            }),
          );
          const previewRes = await API.get('/api/site/group-balance/preview', {
            params: {
              group: balanceGroup,
              mode: balanceMode,
              quota: validBalanceQuota,
            },
            disableDuplicate: true,
          });
          if (previewRes.data.success) {
            setBalancePreview(previewRes.data.data);
          }
        } catch (error) {
          showError(error);
        } finally {
          setBalanceSubmitting(false);
        }
      },
    });
  };

  return (
    <div className='mt-[60px] px-2'>
      <Spin spinning={loading}>
        <Card>
          <Form.Section
            text={t('分组调整')}
            extraText={t('将一个用户分组下的未删除用户批量迁移到另一个分组')}
          >
            <Row gutter={[16, 16]}>
              <Col xs={24} md={10}>
                <Text strong style={{ display: 'block', marginBottom: 8 }}>
                  {t('源分组')}
                </Text>
                <Select
                  value={sourceGroup}
                  optionList={sourceOptions}
                  placeholder={t('选择源分组')}
                  onChange={(value) => setSourceGroup(value || '')}
                  style={{ width: '100%' }}
                  showClear
                />
              </Col>
              <Col xs={24} md={10}>
                <Text strong style={{ display: 'block', marginBottom: 8 }}>
                  {t('目标分组')}
                </Text>
                <Select
                  value={targetGroup}
                  optionList={targetOptions}
                  placeholder={t('选择目标分组')}
                  onChange={(value) => setTargetGroup(value || '')}
                  style={{ width: '100%' }}
                  showClear
                />
              </Col>
              <Col xs={24} md={4}>
                <div
                  style={{
                    display: 'flex',
                    height: '100%',
                    alignItems: 'end',
                  }}
                >
                  <Button
                    type='danger'
                    onClick={executeTransfer}
                    disabled={!canSubmit}
                    loading={submitting}
                    style={{ width: '100%' }}
                  >
                    {t('执行调整')}
                  </Button>
                </div>
              </Col>
            </Row>

            <Banner
              fullMode={false}
              type={validationMessage ? 'warning' : 'info'}
              closeIcon={null}
              style={{ marginTop: 16 }}
              description={
                validationMessage ||
                t('预计将迁移 {{count}} 个未删除用户', {
                  count: previewLoading ? '-' : (previewCount ?? 0),
                })
              }
            />
          </Form.Section>
        </Card>
        <Card style={{ marginTop: 16 }}>
          <Form.Section
            text={t('批量修改分组用户余额')}
            extraText={t('按分组批量增加、减少或覆盖未删除用户的账户余额')}
          >
            <Row gutter={[16, 16]}>
              <Col xs={24} md={8}>
                <Text strong style={{ display: 'block', marginBottom: 8 }}>
                  {t('分组')}
                </Text>
                <Select
                  value={balanceGroup}
                  optionList={sourceOptions}
                  placeholder={t('选择分组')}
                  onChange={(value) => setBalanceGroup(value || '')}
                  style={{ width: '100%' }}
                  showClear
                />
              </Col>
              <Col xs={24} md={8}>
                <Text strong style={{ display: 'block', marginBottom: 8 }}>
                  {t('操作')}
                </Text>
                <RadioGroup
                  type='button'
                  value={balanceMode}
                  onChange={(event) => setBalanceMode(event.target.value)}
                  style={{ width: '100%' }}
                >
                  <Radio value='add'>{balanceModeLabels.add}</Radio>
                  <Radio value='subtract'>{balanceModeLabels.subtract}</Radio>
                  <Radio value='override'>{balanceModeLabels.override}</Radio>
                </RadioGroup>
              </Col>
              <Col xs={24} md={4}>
                <Text strong style={{ display: 'block', marginBottom: 8 }}>
                  {t('金额')}
                </Text>
                <InputNumber
                  value={balanceAmount}
                  min={0}
                  precision={6}
                  step={0.000001}
                  prefix={getCurrencyConfig().symbol}
                  placeholder={t('输入金额')}
                  onChange={(value) =>
                    setBalanceAmount(
                      value === '' || value == null ? null : Number(value),
                    )
                  }
                  style={{ width: '100%' }}
                  showClear
                />
              </Col>
              <Col xs={24} md={4}>
                <div
                  style={{
                    display: 'flex',
                    height: '100%',
                    alignItems: 'end',
                  }}
                >
                  <Button
                    type='danger'
                    onClick={executeBalanceUpdate}
                    disabled={!canSubmitBalance}
                    loading={balanceSubmitting}
                    style={{ width: '100%' }}
                  >
                    {t('执行修改')}
                  </Button>
                </div>
              </Col>
            </Row>

            <Banner
              fullMode={false}
              type={balanceValidationMessage ? 'warning' : 'info'}
              closeIcon={null}
              style={{ marginTop: 16 }}
              description={
                balanceValidationMessage ||
                t('预计影响 {{count}} 个未删除用户，总变化 {{delta}}', {
                  count: balancePreviewLoading
                    ? '-'
                    : (balancePreview?.affected ?? 0),
                  delta: balancePreviewLoading
                    ? '-'
                    : formatQuotaValue(balancePreview?.total_delta ?? 0),
                })
              }
            />
          </Form.Section>
        </Card>
      </Spin>
    </div>
  );
};

export default Site;
