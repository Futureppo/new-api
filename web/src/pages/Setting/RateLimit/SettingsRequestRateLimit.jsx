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

import React, { useEffect, useState, useRef } from 'react';
import { Button, Col, Form, Row, Spin } from '@douyinfe/semi-ui';
import {
  compareObjects,
  API,
  showError,
  showSuccess,
  showWarning,
  verifyJSON,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

const probeGuardDefaults = {
  'probe_guard_setting.enabled': false,
  'probe_guard_setting.dry_run': true,
  'probe_guard_setting.window_seconds': 60,
  'probe_guard_setting.distinct_model_count': 5,
  'probe_guard_setting.first_ip_ban_minutes': 10,
  'probe_guard_setting.second_ip_ban_minutes': 60,
  'probe_guard_setting.permanent_offense_count': 3,
  'probe_guard_setting.offense_dedupe_seconds': 60,
  'probe_guard_setting.whitelist_user_ids': '',
};

export default function RequestRateLimit(props) {
  const { t } = useTranslation();

  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    ModelRequestRateLimitEnabled: false,
    ModelRequestRateLimitCount: -1,
    ModelRequestRateLimitSuccessCount: 1000,
    ModelRequestRateLimitDurationMinutes: 1,
    ModelRequestConcurrencyLimit: 2,
    ModelRequestRateLimitGroup: '',
  });
  const refForm = useRef();
  const [inputsRow, setInputsRow] = useState(inputs);

  function onSubmit() {
    const updateArray = compareObjects(inputs, inputsRow);
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    const requestQueue = updateArray.map((item) => {
      let value = '';
      if (typeof inputs[item.key] === 'boolean') {
        value = String(inputs[item.key]);
      } else {
        value = inputs[item.key];
      }
      return API.put('/api/option/', {
        key: item.key,
        value,
      });
    });
    setLoading(true);
    Promise.all(requestQueue)
      .then((res) => {
        if (requestQueue.length === 1) {
          if (res.includes(undefined)) return;
        } else if (requestQueue.length > 1) {
          if (res.includes(undefined))
            return showError(t('部分保存失败，请重试'));
        }

        for (let i = 0; i < res.length; i++) {
          if (!res[i].data.success) {
            return showError(res[i].data.message);
          }
        }

        showSuccess(t('保存成功'));
        props.refresh();
      })
      .catch(() => {
        showError(t('保存失败，请重试'));
      })
      .finally(() => {
        setLoading(false);
      });
  }

  useEffect(() => {
    const currentInputs = {};
    for (let key in props.options) {
      if (Object.keys(inputs).includes(key)) {
        currentInputs[key] = props.options[key];
      }
    }
    setInputs(currentInputs);
    setInputsRow(structuredClone(currentInputs));
    refForm.current.setValues(currentInputs);
  }, [props.options]);

  return (
    <>
      <Spin spinning={loading}>
        <Form
          values={inputs}
          getFormApi={(formAPI) => (refForm.current = formAPI)}
          style={{ marginBottom: 15 }}
        >
          <Form.Section text={t('模型请求速率限制')}>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'ModelRequestRateLimitEnabled'}
                  label={t('启用用户模型请求速率限制（可能会影响高并发性能）')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={(value) => {
                    setInputs({
                      ...inputs,
                      ModelRequestRateLimitEnabled: value,
                    });
                  }}
                />
              </Col>
            </Row>
            <Row>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('限制周期')}
                  step={1}
                  min={0}
                  suffix={t('分钟')}
                  extraText={t('频率限制的周期（分钟）')}
                  field={'ModelRequestRateLimitDurationMinutes'}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      ModelRequestRateLimitDurationMinutes: String(value),
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('用户最大并发请求数')}
                  step={1}
                  min={0}
                  max={100000000}
                  suffix={t('个')}
                  extraText={t(
                    '同一用户正在处理中的模型请求数上限，0代表不限制',
                  )}
                  field={'ModelRequestConcurrencyLimit'}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      ModelRequestConcurrencyLimit: String(value),
                    })
                  }
                />
              </Col>
            </Row>
            <Row>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('用户每周期最多请求次数')}
                  step={1}
                  min={0}
                  max={100000000}
                  suffix={t('次')}
                  extraText={t('包括失败请求的次数，0代表不限制')}
                  field={'ModelRequestRateLimitCount'}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      ModelRequestRateLimitCount: String(value),
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('用户每周期最多请求完成次数')}
                  step={1}
                  min={1}
                  max={100000000}
                  suffix={t('次')}
                  extraText={t('只包括请求成功的次数')}
                  field={'ModelRequestRateLimitSuccessCount'}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      ModelRequestRateLimitSuccessCount: String(value),
                    })
                  }
                />
              </Col>
            </Row>
            <Row>
              <Col xs={24} sm={16}>
                <Form.TextArea
                  label={t('分组速率限制')}
                  placeholder={t(
                    '{\n  "default": [200, 100, 2],\n  "vip": [0, 1000, 10]\n}',
                  )}
                  field={'ModelRequestRateLimitGroup'}
                  autosize={{ minRows: 5, maxRows: 15 }}
                  trigger='blur'
                  stopValidateWithError
                  rules={[
                    {
                      validator: (rule, value) => verifyJSON(value),
                      message: t('不是合法的 JSON 字符串'),
                    },
                  ]}
                  extraText={
                    <div>
                      <p>{t('说明：')}</p>
                      <ul>
                        <li>
                          {t(
                            '使用 JSON 对象格式，格式为：{"组名": [最多请求次数, 最多请求完成次数, 最大并发数]}',
                          )}
                        </li>
                        <li>
                          {t(
                            '示例：{"default": [200, 100, 2], "vip": [0, 1000, 10]}。',
                          )}
                        </li>
                        <li>
                          {t(
                            '[最多请求次数]必须大于等于0，[最多请求完成次数]必须大于等于1。',
                          )}
                        </li>
                        <li>
                          {t(
                            '[最大并发数]必须大于等于0，0代表不限制。',
                          )}
                        </li>
                        <li>
                          {t(
                            '[最多请求次数]、[最多请求完成次数]和[最大并发数]的最大值为2147483647。',
                          )}
                        </li>
                        <li>{t('旧的两项数组配置会使用全局并发限制。')}</li>
                        <li>{t('分组速率配置优先级高于全局速率限制。')}</li>
                        <li>{t('限制周期统一使用上方配置的“限制周期”值。')}</li>
                      </ul>
                    </div>
                  }
                  onChange={(value) => {
                    setInputs({ ...inputs, ModelRequestRateLimitGroup: value });
                  }}
                />
              </Col>
            </Row>
            <Row>
              <Button size='default' onClick={onSubmit}>
                {t('保存模型速率限制')}
              </Button>
            </Row>
          </Form.Section>
        </Form>
      </Spin>
    </>
  );
}

export function ProbeGuardRateLimit(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState(probeGuardDefaults);
  const [inputsRow, setInputsRow] = useState(probeGuardDefaults);
  const refForm = useRef();

  function handleFieldChange(key) {
    return (value) => {
      setInputs({
        ...inputs,
        [key]: typeof value === 'boolean' ? value : String(value),
      });
    };
  }

  function onSubmit() {
    const updateArray = compareObjects(inputs, inputsRow);
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    const requestQueue = updateArray.map((item) => {
      const value =
        typeof inputs[item.key] === 'boolean'
          ? String(inputs[item.key])
          : inputs[item.key];
      return API.put('/api/option/', {
        key: item.key,
        value,
      });
    });
    setLoading(true);
    Promise.all(requestQueue)
      .then((res) => {
        if (res.includes(undefined)) {
          return showError(t('部分保存失败，请重试'));
        }
        for (let i = 0; i < res.length; i++) {
          if (!res[i].data.success) {
            return showError(res[i].data.message);
          }
        }
        showSuccess(t('保存成功'));
        props.refresh();
      })
      .catch(() => {
        showError(t('保存失败，请重试'));
      })
      .finally(() => {
        setLoading(false);
      });
  }

  useEffect(() => {
    const currentInputs = { ...probeGuardDefaults };
    for (let key in props.options) {
      if (Object.keys(probeGuardDefaults).includes(key)) {
        currentInputs[key] = props.options[key];
      }
    }
    setInputs(currentInputs);
    setInputsRow(structuredClone(currentInputs));
    refForm.current?.setValues(currentInputs);
  }, [props.options]);

  const enabled = !!inputs['probe_guard_setting.enabled'];

  return (
    <Spin spinning={loading}>
      <Form
        values={inputs}
        getFormApi={(formAPI) => (refForm.current = formAPI)}
        style={{ marginBottom: 15 }}
      >
        <Form.Section
          text={t('批量测活防护')}
          extraText={t(
            '同一 IP 在检测窗口内请求过多不同模型时，自动按违规次数封禁该 IP',
          )}
        >
          <Row gutter={16}>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.Switch
                field='probe_guard_setting.enabled'
                label={t('启用批量测活防护')}
                checkedText={t('开')}
                uncheckedText={t('关')}
                onChange={handleFieldChange('probe_guard_setting.enabled')}
              />
            </Col>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.Switch
                field='probe_guard_setting.dry_run'
                label={t('试运行')}
                extraText={t('只记录触发情况，不写入 IP 封禁，也不禁用账号')}
                checkedText={t('开')}
                uncheckedText={t('关')}
                disabled={!enabled}
                onChange={handleFieldChange('probe_guard_setting.dry_run')}
              />
            </Col>
          </Row>
          <Row gutter={16}>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.InputNumber
                field='probe_guard_setting.window_seconds'
                label={t('检测窗口')}
                min={10}
                max={3600}
                step={1}
                suffix={t('秒')}
                disabled={!enabled}
                onChange={handleFieldChange('probe_guard_setting.window_seconds')}
              />
            </Col>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.InputNumber
                field='probe_guard_setting.distinct_model_count'
                label={t('不同模型阈值')}
                min={2}
                max={1000}
                step={1}
                suffix={t('个')}
                disabled={!enabled}
                onChange={handleFieldChange(
                  'probe_guard_setting.distinct_model_count',
                )}
              />
            </Col>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.InputNumber
                field='probe_guard_setting.offense_dedupe_seconds'
                label={t('同批去重时间')}
                min={10}
                max={3600}
                step={1}
                suffix={t('秒')}
                disabled={!enabled}
                onChange={handleFieldChange(
                  'probe_guard_setting.offense_dedupe_seconds',
                )}
              />
            </Col>
          </Row>
          <Row gutter={16}>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.InputNumber
                field='probe_guard_setting.first_ip_ban_minutes'
                label={t('第一次封禁 IP')}
                min={1}
                max={43200}
                step={1}
                suffix={t('分钟')}
                disabled={!enabled}
                onChange={handleFieldChange(
                  'probe_guard_setting.first_ip_ban_minutes',
                )}
              />
            </Col>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.InputNumber
                field='probe_guard_setting.second_ip_ban_minutes'
                label={t('第二次封禁 IP')}
                min={1}
                max={43200}
                step={1}
                suffix={t('分钟')}
                disabled={!enabled}
                onChange={handleFieldChange(
                  'probe_guard_setting.second_ip_ban_minutes',
                )}
              />
            </Col>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.InputNumber
                field='probe_guard_setting.permanent_offense_count'
                label={t('永久封禁触发次数')}
                min={1}
                max={100}
                step={1}
                suffix={t('次')}
                disabled={!enabled}
                onChange={handleFieldChange(
                  'probe_guard_setting.permanent_offense_count',
                )}
              />
            </Col>
          </Row>
          <Row gutter={16}>
            <Col xs={24} sm={24} md={16} lg={16} xl={16}>
              <Form.TextArea
                field='probe_guard_setting.whitelist_user_ids'
                label={t('用户白名单')}
                placeholder={t('输入用户 ID，支持逗号、空格或换行分隔，如：1, 2, 3')}
                autosize={{ minRows: 3, maxRows: 8 }}
                disabled={!enabled}
                extraText={t('白名单用户会完全跳过批量测活检测；管理员和超级管理员默认跳过')}
                onChange={handleFieldChange(
                  'probe_guard_setting.whitelist_user_ids',
                )}
              />
            </Col>
          </Row>
          <Row>
            <Button size='default' onClick={onSubmit}>
              {t('保存批量测活防护')}
            </Button>
          </Row>
        </Form.Section>
      </Form>
    </Spin>
  );
}
