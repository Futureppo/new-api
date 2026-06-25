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
import { useTranslation } from 'react-i18next';
import {
  compareObjects,
  API,
  getCurrencyConfig,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';
import {
  displayAmountToQuota,
  quotaToDisplayAmount,
} from '../../../helpers/quota';

const quotaAmountFields = [
  'QuotaForNewUser',
  'PreConsumedQuota',
  'QuotaForInviter',
  'QuotaForInvitee',
];

function quotaToAmountValue(quota) {
  return Number(
    quotaToDisplayAmount(quota, { forceCurrency: true }).toFixed(6),
  );
}

function getAmountInputs(inputs) {
  const amountInputs = { ...inputs };
  for (const field of quotaAmountFields) {
    if (amountInputs[field] !== undefined) {
      amountInputs[field] = quotaToAmountValue(amountInputs[field]);
    }
  }
  return amountInputs;
}

export default function SettingsCreditLimit(props) {
  const { t } = useTranslation();
  const currencySymbol = getCurrencyConfig().symbol;
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    QuotaForNewUser: '',
    PreConsumedQuota: '',
    QuotaForInviter: '',
    QuotaForInvitee: '',
    'quota_setting.enable_free_model_pre_consume': true,
  });
  const refForm = useRef();
  const [inputsRow, setInputsRow] = useState(inputs);

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
          <Form.Section text={t('金额设置')}>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('新用户初始金额')}
                  field={'QuotaForNewUser'}
                  step={0.000001}
                  precision={6}
                  min={0}
                  prefix={currencySymbol}
                  placeholder={t('输入金额')}
                  onChange={handleAmountFieldChange('QuotaForNewUser')}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('请求预扣费金额')}
                  field={'PreConsumedQuota'}
                  step={0.000001}
                  precision={6}
                  min={0}
                  prefix={currencySymbol}
                  extraText={t('请求结束后多退少补')}
                  placeholder={t('输入金额')}
                  onChange={handleAmountFieldChange('PreConsumedQuota')}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('邀请新用户奖励金额')}
                  field={'QuotaForInviter'}
                  step={0.000001}
                  precision={6}
                  min={0}
                  prefix={currencySymbol}
                  extraText={''}
                  placeholder={t('输入金额')}
                  onChange={handleAmountFieldChange('QuotaForInviter')}
                />
              </Col>
            </Row>
            <Row>
              <Col xs={24} sm={12} md={8} lg={8} xl={6}>
                <Form.InputNumber
                  label={t('新用户使用邀请码奖励金额')}
                  field={'QuotaForInvitee'}
                  step={0.000001}
                  precision={6}
                  min={0}
                  prefix={currencySymbol}
                  extraText={''}
                  placeholder={t('输入金额')}
                  onChange={handleAmountFieldChange('QuotaForInvitee')}
                />
              </Col>
            </Row>
            <Row>
              <Col>
                <Form.Switch
                  label={t('对免费模型启用预消耗')}
                  field={'quota_setting.enable_free_model_pre_consume'}
                  extraText={t(
                    '开启后，对免费模型（倍率为0，或者价格为0）的模型也会预消耗金额',
                  )}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'quota_setting.enable_free_model_pre_consume': value,
                    })
                  }
                />
              </Col>
            </Row>

            <Row>
              <Button size='default' onClick={onSubmit}>
                {t('保存金额设置')}
              </Button>
            </Row>
          </Form.Section>
        </Form>
      </Spin>
    </>
  );
}
