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
import { Button, Col, Form, Row, Spin, Typography } from '@douyinfe/semi-ui';
import {
  compareObjects,
  API,
  getCurrencyConfig,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';
import {
  displayAmountToQuota,
  quotaToDisplayAmount,
} from '../../../helpers/quota';

const checkinAmountFields = [
  'checkin_setting.min_quota',
  'checkin_setting.max_quota',
  'checkin_setting.special_quota',
];

const defaultInputs = {
  'checkin_setting.enabled': false,
  'checkin_setting.min_quota': 1000,
  'checkin_setting.max_quota': 10000,
  'checkin_setting.special_enabled': false,
  'checkin_setting.special_weekday': '1',
  'checkin_setting.special_quota': 0,
};

const weekdayOptions = [
  { value: '1', label: '周一' },
  { value: '2', label: '周二' },
  { value: '3', label: '周三' },
  { value: '4', label: '周四' },
  { value: '5', label: '周五' },
  { value: '6', label: '周六' },
  { value: '7', label: '周日' },
];

function quotaToAmountValue(quota) {
  return Number(
    quotaToDisplayAmount(quota, { forceCurrency: true }).toFixed(6),
  );
}

function getAmountInputs(inputs) {
  const amountInputs = { ...inputs };
  for (const field of checkinAmountFields) {
    if (amountInputs[field] !== undefined) {
      amountInputs[field] = quotaToAmountValue(amountInputs[field]);
    }
  }
  return amountInputs;
}

export default function SettingsCheckin(props) {
  const { t } = useTranslation();
  const currencySymbol = getCurrencyConfig().symbol;
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState(defaultInputs);
  const refForm = useRef();
  const [inputsRow, setInputsRow] = useState(inputs);

  function handleFieldChange(fieldName) {
    return (value) => {
      setInputs((inputs) => ({ ...inputs, [fieldName]: value }));
    };
  }

  function handleAmountFieldChange(fieldName) {
    return (value) => {
      const amount = value === '' || value == null ? 0 : value;
      setInputs((inputs) => ({
        ...inputs,
        [fieldName]: String(
          displayAmountToQuota(amount, { forceCurrency: true }),
        ),
      }));
    };
  }

  function onSubmit() {
    const updateArray = compareObjects(inputs, inputsRow);
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    const requestQueue = updateArray.map((item) => {
      let value = '';
      if (typeof inputs[item.key] === 'boolean') {
        value = String(inputs[item.key]);
      } else {
        value = String(inputs[item.key]);
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
    const currentInputs = { ...defaultInputs };
    for (let key in props.options) {
      if (Object.keys(inputs).includes(key)) {
        currentInputs[key] = props.options[key];
      }
    }
    setInputs(currentInputs);
    setInputsRow(structuredClone(currentInputs));
    refForm.current.setValues(getAmountInputs(currentInputs));
  }, [props.options]);

  return (
    <>
      <Spin spinning={loading}>
        <Form
          values={getAmountInputs(inputs)}
          getFormApi={(formAPI) => (refForm.current = formAPI)}
          style={{ marginBottom: 15 }}
        >
          <Form.Section text={t('签到设置')}>
            <Typography.Text
              type='tertiary'
              style={{ marginBottom: 16, display: 'block' }}
            >
              {t('签到功能允许用户每日签到获取金额奖励，特殊星期可覆盖为固定奖励')}
            </Typography.Text>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'checkin_setting.enabled'}
                  label={t('启用签到功能')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={handleFieldChange('checkin_setting.enabled')}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  field={'checkin_setting.min_quota'}
                  label={t('签到最小金额')}
                  placeholder={t('签到奖励的最小金额')}
                  onChange={handleAmountFieldChange(
                    'checkin_setting.min_quota',
                  )}
                  prefix={currencySymbol}
                  precision={6}
                  step={0.000001}
                  min={0}
                  disabled={!inputs['checkin_setting.enabled']}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  field={'checkin_setting.max_quota'}
                  label={t('签到最大金额')}
                  placeholder={t('签到奖励的最大金额')}
                  onChange={handleAmountFieldChange(
                    'checkin_setting.max_quota',
                  )}
                  prefix={currencySymbol}
                  precision={6}
                  step={0.000001}
                  min={0}
                  disabled={!inputs['checkin_setting.enabled']}
                />
              </Col>
            </Row>
            <Typography.Text
              strong
              style={{ marginTop: 8, marginBottom: 8, display: 'block' }}
            >
              {t('特殊签到设置')}
            </Typography.Text>
            <Typography.Text
              type='tertiary'
              style={{ marginBottom: 16, display: 'block' }}
            >
              {t('启用后，命中指定星期时将发放固定金额并覆盖随机奖励')}
            </Typography.Text>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'checkin_setting.special_enabled'}
                  label={t('启用特殊签到奖励')}
                  size='default'
                  onChange={handleFieldChange(
                    'checkin_setting.special_enabled',
                  )}
                  disabled={!inputs['checkin_setting.enabled']}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Select
                  field={'checkin_setting.special_weekday'}
                  label={t('特殊签到星期')}
                  onChange={handleFieldChange(
                    'checkin_setting.special_weekday',
                  )}
                  disabled={
                    !inputs['checkin_setting.enabled'] ||
                    !inputs['checkin_setting.special_enabled']
                  }
                >
                  {weekdayOptions.map((item) => (
                    <Form.Select.Option key={item.value} value={item.value}>
                      {t(item.label)}
                    </Form.Select.Option>
                  ))}
                </Form.Select>
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  field={'checkin_setting.special_quota'}
                  label={t('特殊签到金额')}
                  placeholder={t('特殊签到固定奖励金额')}
                  onChange={handleAmountFieldChange(
                    'checkin_setting.special_quota',
                  )}
                  prefix={currencySymbol}
                  precision={6}
                  step={0.000001}
                  min={0}
                  disabled={
                    !inputs['checkin_setting.enabled'] ||
                    !inputs['checkin_setting.special_enabled']
                  }
                />
              </Col>
            </Row>
            <Row>
              <Button size='default' onClick={onSubmit}>
                {t('保存签到设置')}
              </Button>
            </Row>
          </Form.Section>
        </Form>
      </Spin>
    </>
  );
}
