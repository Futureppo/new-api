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

import React, {
  useCallback,
  useEffect,
  useState,
  useContext,
  useRef,
} from 'react';
import {
  API,
  showError,
  showSuccess,
  timestamp2string,
  renderGroupOption,
  getCurrencyConfig,
  getModelCategories,
  selectFilter,
} from '../../../../helpers';
import {
  quotaToDisplayAmount,
  displayAmountToQuota,
} from '../../../../helpers/quota';
import { useIsMobile } from '../../../../hooks/common/useIsMobile';
import {
  Button,
  SideSheet,
  Space,
  Spin,
  Typography,
  Card,
  Tag,
  Avatar,
  Form,
  Col,
  Row,
} from '@douyinfe/semi-ui';
import {
  IconCreditCard,
  IconLink,
  IconSave,
  IconClose,
  IconKey,
} from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { StatusContext } from '../../../../context/Status';

const { Text, Title } = Typography;

const getTokenFormInitValues = () => ({
  name: '',
  remain_quota: 0,
  remain_amount: 0,
  expired_time: -1,
  unlimited_quota: true,
  model_limits_enabled: false,
  model_limits: [],
  allow_ips: '',
  groups: [],
  cross_group_retry: false,
  tokenCount: 1,
});

const normalizeGroupValues = (values) => {
  if (!Array.isArray(values)) return [];
  return [
    ...new Set(
      values
        .filter((value) => typeof value === 'string')
        .map((value) => value.trim())
        .filter(Boolean),
    ),
  ];
};

const EditTokenModal = (props) => {
  const { t } = useTranslation();
  const [statusState] = useContext(StatusContext);
  const [loading, setLoading] = useState(false);
  const isMobile = useIsMobile();
  const formApiRef = useRef(null);
  const initializationRequestIdRef = useRef(0);
  const modelRequestIdRef = useRef(0);
  const groupSelectionRef = useRef([]);
  const validGroupValuesRef = useRef(new Set());
  const [models, setModels] = useState([]);
  const [groups, setGroups] = useState([]);
  const [unavailableGroups, setUnavailableGroups] = useState([]);
  const [showQuotaInput, setShowQuotaInput] = useState(false);
  const isEdit = props.editingToken.id !== undefined;

  const groupOptions = [
    ...groups,
    ...unavailableGroups.map((group) => ({
      label: t('已失效或不可用'),
      value: group,
      disabled: true,
      unavailable: true,
    })),
  ];

  const getValidGroups = useCallback(
    (values) =>
      normalizeGroupValues(values).filter((group) =>
        validGroupValuesRef.current.has(group),
      ),
    [],
  );

  const getRequestErrorMessage = useCallback(
    (error, fallback) => {
      const message = error?.response?.data?.message || error?.message;
      return message ? t(message) : fallback;
    },
    [t],
  );

  const handleCancel = () => {
    props.handleClose();
  };

  const setExpiredTime = (month, day, hour, minute) => {
    let now = new Date();
    let timestamp = now.getTime() / 1000;
    let seconds = month * 30 * 24 * 60 * 60;
    seconds += day * 24 * 60 * 60;
    seconds += hour * 60 * 60;
    seconds += minute * 60;
    if (!formApiRef.current) return;
    if (seconds !== 0) {
      timestamp += seconds;
      formApiRef.current.setValue('expired_time', timestamp2string(timestamp));
    } else {
      formApiRef.current.setValue('expired_time', -1);
    }
  };

  const loadModels = useCallback(
    async (selectedGroups = [], selectedModels) => {
      const requestId = ++modelRequestIdRef.current;
      const validGroups = normalizeGroupValues(selectedGroups).filter((group) =>
        validGroupValuesRef.current.has(group),
      );
      const effectiveGroups = validGroups.includes('auto')
        ? ['auto']
        : validGroups;

      try {
        const res = await API.get(`/api/user/models`, {
          params: { groups: JSON.stringify(effectiveGroups) },
          skipErrorHandler: true,
        });
        if (requestId !== modelRequestIdRef.current) return;

        const { success, message, data } = res.data;
        if (!success) {
          showError(t(message || '加载模型失败'));
          return;
        }

        const modelList = Array.isArray(data) ? data : [];

        const categories = getModelCategories(t);
        const localModelOptions = modelList.map((model) => {
          let icon = null;
          for (const [key, category] of Object.entries(categories)) {
            if (key !== 'all' && category.filter({ model_name: model })) {
              icon = category.icon;
              break;
            }
          }
          return {
            label: (
              <span className='flex items-center gap-1'>
                {icon}
                {model}
              </span>
            ),
            value: model,
          };
        });
        setModels(localModelOptions);

        const currentModelLimits = Array.isArray(selectedModels)
          ? selectedModels
          : formApiRef.current?.getValue('model_limits') || [];
        const allowedModels = new Set(modelList);
        const filteredModelLimits = currentModelLimits.filter((model) =>
          allowedModels.has(model),
        );
        if (filteredModelLimits.length !== currentModelLimits.length) {
          formApiRef.current?.setValue('model_limits', filteredModelLimits);
        }
      } catch (error) {
        if (requestId !== modelRequestIdRef.current) return;
        showError(getRequestErrorMessage(error, t('加载模型失败')));
      }
    },
    [getRequestErrorMessage, t],
  );

  useEffect(() => {
    const requestId = ++initializationRequestIdRef.current;
    modelRequestIdRef.current += 1;

    if (!props.visiable) {
      setLoading(false);
      setModels([]);
      setGroups([]);
      setUnavailableGroups([]);
      validGroupValuesRef.current = new Set();
      groupSelectionRef.current = [];
      formApiRef.current?.reset();
      return undefined;
    }

    const initializeForm = async () => {
      setLoading(true);
      setModels([]);
      setGroups([]);
      setUnavailableGroups([]);
      validGroupValuesRef.current = new Set();
      groupSelectionRef.current = [];
      formApiRef.current?.setValues(getTokenFormInitValues());

      try {
        const groupRequest = API.get(`/api/user/self/groups`, {
          skipErrorHandler: true,
        });
        const tokenRequest = isEdit
          ? API.get(`/api/token/${props.editingToken.id}`, {
              skipErrorHandler: true,
            })
          : Promise.resolve(null);
        const [groupResponse, tokenResponse] = await Promise.all([
          groupRequest,
          tokenRequest,
        ]);
        if (requestId !== initializationRequestIdRef.current) return;

        const groupPayload = groupResponse?.data;
        if (!groupPayload?.success || !groupPayload.data) {
          throw new Error(t(groupPayload?.message || '加载分组失败'));
        }

        const localGroupOptions = Object.entries(groupPayload.data).map(
          ([group, info]) => ({
            label: info.desc,
            value: group,
            ratio: info.ratio,
          }),
        );
        if (
          statusState?.status?.default_use_auto_group &&
          localGroupOptions.some((group) => group.value === 'auto')
        ) {
          localGroupOptions.sort((a, b) => {
            if (a.value === 'auto') return -1;
            if (b.value === 'auto') return 1;
            return 0;
          });
        }

        const validGroupValues = new Set(
          localGroupOptions.map((group) => group.value),
        );
        validGroupValuesRef.current = validGroupValues;
        setGroups(localGroupOptions);

        if (!isEdit) {
          formApiRef.current?.setValues(getTokenFormInitValues());
          await loadModels([]);
          return;
        }

        const tokenPayload = tokenResponse?.data;
        if (!tokenPayload?.success || !tokenPayload.data) {
          throw new Error(t(tokenPayload?.message || '加载令牌失败'));
        }

        const tokenData = { ...tokenPayload.data };
        if (tokenData.expired_time !== -1) {
          tokenData.expired_time = timestamp2string(tokenData.expired_time);
        }
        tokenData.model_limits =
          typeof tokenData.model_limits === 'string' &&
          tokenData.model_limits !== ''
            ? tokenData.model_limits.split(',').filter(Boolean)
            : [];
        tokenData.remain_amount = Number(
          quotaToDisplayAmount(tokenData.remain_quota || 0).toFixed(6),
        );
        tokenData.groups = normalizeGroupValues(
          Array.isArray(tokenData.groups)
            ? tokenData.groups
            : tokenData.group
              ? [tokenData.group]
              : [],
        );

        const invalidGroups = tokenData.groups.filter(
          (group) => !validGroupValues.has(group),
        );
        const effectiveGroups = tokenData.groups.filter((group) =>
          validGroupValues.has(group),
        );
        setUnavailableGroups(invalidGroups);
        groupSelectionRef.current = tokenData.groups;
        formApiRef.current?.setValues({
          ...getTokenFormInitValues(),
          ...tokenData,
        });
        await loadModels(effectiveGroups, tokenData.model_limits);
      } catch (error) {
        if (requestId !== initializationRequestIdRef.current) return;
        showError(getRequestErrorMessage(error, t('操作失败，请重试')));
      } finally {
        if (requestId === initializationRequestIdRef.current) {
          setLoading(false);
        }
      }
    };

    initializeForm();

    return () => {
      initializationRequestIdRef.current += 1;
      modelRequestIdRef.current += 1;
    };
  }, [
    getRequestErrorMessage,
    isEdit,
    loadModels,
    props.editingToken.id,
    props.visiable,
    statusState?.status?.default_use_auto_group,
    t,
  ]);

  const generateRandomSuffix = () => {
    const characters =
      'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
    let result = '';
    for (let i = 0; i < 6; i++) {
      result += characters.charAt(
        Math.floor(Math.random() * characters.length),
      );
    }
    return result;
  };

  const handleGroupChange = (value) => {
    const previousGroups = normalizeGroupValues(groupSelectionRef.current);
    const rawGroups = normalizeGroupValues(value);
    let nextGroups = [
      ...previousGroups.filter((group) => rawGroups.includes(group)),
      ...rawGroups.filter((group) => !previousGroups.includes(group)),
    ];

    const previousValidGroups = getValidGroups(previousGroups);
    const nextValidGroups = getValidGroups(nextGroups);
    if (nextValidGroups.includes('auto') && nextValidGroups.length > 1) {
      const keepAuto = !previousValidGroups.includes('auto');
      nextGroups = nextGroups.filter((group) => {
        if (!validGroupValuesRef.current.has(group)) return true;
        return keepAuto ? group === 'auto' : group !== 'auto';
      });
    }

    const effectiveGroups = getValidGroups(nextGroups);
    groupSelectionRef.current = nextGroups;
    setUnavailableGroups((current) =>
      current.filter((group) => nextGroups.includes(group)),
    );
    formApiRef.current?.setValue('groups', nextGroups);
    loadModels(effectiveGroups);
  };

  const submit = async (values) => {
    const selectedGroups = normalizeGroupValues(values.groups);
    const invalidGroups = selectedGroups.filter(
      (group) => !validGroupValuesRef.current.has(group),
    );
    if (invalidGroups.length > 0) {
      showError(
        t('该令牌包含已失效或不可用的分组，请先移除后再提交：{{groups}}', {
          groups: invalidGroups.join(', '),
        }),
      );
      return;
    }

    setLoading(true);
    try {
      if (isEdit) {
        let { tokenCount: _tc, ...localInputs } = values;
        localInputs.groups = selectedGroups;
        localInputs.group = localInputs.groups[0] || '';
        localInputs.remain_quota = localInputs.unlimited_quota
          ? 0
          : displayAmountToQuota(localInputs.remain_amount);
        if (!localInputs.unlimited_quota && localInputs.remain_quota <= 0) {
          showError(t('请输入金额'));
          return;
        }
        if (localInputs.expired_time !== -1) {
          let time = Date.parse(localInputs.expired_time);
          if (isNaN(time)) {
            showError(t('过期时间格式错误！'));
            return;
          }
          localInputs.expired_time = Math.ceil(time / 1000);
        }
        localInputs.model_limits = localInputs.model_limits.join(',');
        localInputs.model_limits_enabled = localInputs.model_limits.length > 0;
        const res = await API.put(
          `/api/token/`,
          {
            ...localInputs,
            id: parseInt(props.editingToken.id),
          },
          { skipErrorHandler: true },
        );
        const { success, message } = res.data;
        if (success) {
          showSuccess(t('令牌更新成功！'));
          props.refresh();
          props.handleClose();
        } else {
          showError(t(message));
        }
      } else {
        const count = parseInt(values.tokenCount, 10) || 1;
        let successCount = 0;
        for (let i = 0; i < count; i++) {
          let { tokenCount: _tc, ...localInputs } = values;
          localInputs.groups = selectedGroups;
          localInputs.group = localInputs.groups[0] || '';
          const baseName =
            values.name.trim() === '' ? 'default' : values.name.trim();
          if (i !== 0 || values.name.trim() === '') {
            localInputs.name = `${baseName}-${generateRandomSuffix()}`;
          } else {
            localInputs.name = baseName;
          }
          localInputs.remain_quota = localInputs.unlimited_quota
            ? 0
            : displayAmountToQuota(localInputs.remain_amount);
          if (!localInputs.unlimited_quota && localInputs.remain_quota <= 0) {
            showError(t('请输入金额'));
            break;
          }

          if (localInputs.expired_time !== -1) {
            let time = Date.parse(localInputs.expired_time);
            if (isNaN(time)) {
              showError(t('过期时间格式错误！'));
              break;
            }
            localInputs.expired_time = Math.ceil(time / 1000);
          }
          localInputs.model_limits = localInputs.model_limits.join(',');
          localInputs.model_limits_enabled =
            localInputs.model_limits.length > 0;
          const res = await API.post(`/api/token/`, localInputs, {
            skipErrorHandler: true,
          });
          const { success, message } = res.data;
          if (success) {
            successCount++;
          } else {
            showError(t(message));
            break;
          }
        }
        if (successCount > 0) {
          showSuccess(t('令牌创建成功，请在列表页面点击复制获取令牌！'));
          props.refresh();
          props.handleClose();
        }
      }
    } catch (error) {
      showError(getRequestErrorMessage(error, t('操作失败，请重试')));
    } finally {
      setLoading(false);
    }
  };

  return (
    <SideSheet
      placement={isEdit ? 'right' : 'left'}
      title={
        <Space>
          {isEdit ? (
            <Tag color='blue' shape='circle'>
              {t('更新')}
            </Tag>
          ) : (
            <Tag color='green' shape='circle'>
              {t('新建')}
            </Tag>
          )}
          <Title heading={4} className='m-0'>
            {isEdit ? t('更新令牌信息') : t('创建新的令牌')}
          </Title>
        </Space>
      }
      bodyStyle={{ padding: '0' }}
      visible={props.visiable}
      width={isMobile ? '100%' : 600}
      footer={
        <div className='flex justify-end bg-white'>
          <Space>
            <Button
              theme='solid'
              className='!rounded-lg'
              onClick={() => formApiRef.current?.submitForm()}
              icon={<IconSave />}
              loading={loading}
            >
              {t('提交')}
            </Button>
            <Button
              theme='light'
              className='!rounded-lg'
              type='primary'
              onClick={handleCancel}
              icon={<IconClose />}
            >
              {t('取消')}
            </Button>
          </Space>
        </div>
      }
      closeIcon={null}
      onCancel={() => handleCancel()}
    >
      <Spin spinning={loading}>
        <Form
          key={isEdit ? 'edit' : 'new'}
          initValues={getTokenFormInitValues()}
          getFormApi={(api) => (formApiRef.current = api)}
          onSubmit={submit}
        >
          {({ values }) => (
            <div className='p-2'>
              {/* 基本信息 */}
              <Card className='!rounded-2xl shadow-sm border-0'>
                <div className='flex items-center mb-2'>
                  <Avatar size='small' color='blue' className='mr-2 shadow-md'>
                    <IconKey size={16} />
                  </Avatar>
                  <div>
                    <Text className='text-lg font-medium'>{t('基本信息')}</Text>
                    <div className='text-xs text-gray-600'>
                      {t('设置令牌的基本信息')}
                    </div>
                  </div>
                </div>
                <Row gutter={12}>
                  <Col span={24}>
                    <Form.Input
                      field='name'
                      label={t('名称')}
                      placeholder={t('请输入名称')}
                      rules={[{ required: true, message: t('请输入名称') }]}
                      showClear
                    />
                  </Col>
                  <Col span={24}>
                    {groupOptions.length > 0 ? (
                      <Form.Select
                        field='groups'
                        label={t('令牌分组')}
                        placeholder={t('令牌分组，默认为用户的分组')}
                        optionList={groupOptions}
                        multiple
                        renderOptionItem={renderGroupOption}
                        renderSelectedItem={(optionNode, multipleProps) => {
                          if (!optionNode?.unavailable) {
                            return {
                              isRenderInTag: true,
                              content: optionNode?.label || optionNode?.value,
                            };
                          }
                          return {
                            isRenderInTag: false,
                            content: (
                              <Tag
                                color='red'
                                type='light'
                                size='large'
                                closable
                                aria-label={t('移除失效分组 {{group}}', {
                                  group: optionNode.value,
                                })}
                                onClose={(content, event) =>
                                  multipleProps.onClose(content, event)
                                }
                              >
                                {optionNode.value} · {t('已失效或不可用')}
                              </Tag>
                            ),
                          };
                        }}
                        filter={(input, option) => {
                          const q = input.toLowerCase();
                          return (
                            option.value?.toLowerCase().includes(q) ||
                            (typeof option.label === 'string' &&
                              option.label.toLowerCase().includes(q))
                          );
                        }}
                        onChange={handleGroupChange}
                        autoClearSearchValue={false}
                        showClear
                        extraText={
                          unavailableGroups.length > 0
                            ? t(
                                '该令牌包含已失效或不可用的分组，请先移除后再提交：{{groups}}',
                                { groups: unavailableGroups.join(', ') },
                              )
                            : undefined
                        }
                        style={{ width: '100%' }}
                      />
                    ) : (
                      <Form.Select
                        placeholder={t('管理员未设置用户可选分组')}
                        disabled
                        label={t('令牌分组')}
                        style={{ width: '100%' }}
                      />
                    )}
                  </Col>
                  <Col
                    span={24}
                    style={{
                      display:
                        getValidGroups(values.groups).includes('auto') ||
                        getValidGroups(values.groups).length > 1
                          ? 'block'
                          : 'none',
                    }}
                  >
                    <Form.Switch
                      field='cross_group_retry'
                      label={t('跨分组重试')}
                      size='default'
                      extraText={t(
                        '开启后，当前分组渠道失败时会按顺序尝试下一个分组的渠道',
                      )}
                    />
                  </Col>
                  <Col xs={24} sm={24} md={24} lg={10} xl={10}>
                    <Form.DatePicker
                      field='expired_time'
                      label={t('过期时间')}
                      type='dateTime'
                      placeholder={t('请选择过期时间')}
                      rules={[
                        { required: true, message: t('请选择过期时间') },
                        {
                          validator: (rule, value) => {
                            // 允许 -1 表示永不过期，也允许空值在必填校验时被拦截
                            if (value === -1 || !value)
                              return Promise.resolve();
                            const time = Date.parse(value);
                            if (isNaN(time)) {
                              return Promise.reject(t('过期时间格式错误！'));
                            }
                            if (time <= Date.now()) {
                              return Promise.reject(
                                t('过期时间不能早于当前时间！'),
                              );
                            }
                            return Promise.resolve();
                          },
                        },
                      ]}
                      showClear
                      style={{ width: '100%' }}
                    />
                  </Col>
                  <Col xs={24} sm={24} md={24} lg={14} xl={14}>
                    <Form.Slot label={t('过期时间快捷设置')}>
                      <Space wrap>
                        <Button
                          theme='light'
                          type='primary'
                          onClick={() => setExpiredTime(0, 0, 0, 0)}
                        >
                          {t('永不过期')}
                        </Button>
                        <Button
                          theme='light'
                          type='tertiary'
                          onClick={() => setExpiredTime(1, 0, 0, 0)}
                        >
                          {t('一个月')}
                        </Button>
                        <Button
                          theme='light'
                          type='tertiary'
                          onClick={() => setExpiredTime(0, 1, 0, 0)}
                        >
                          {t('一天')}
                        </Button>
                        <Button
                          theme='light'
                          type='tertiary'
                          onClick={() => setExpiredTime(0, 0, 1, 0)}
                        >
                          {t('一小时')}
                        </Button>
                      </Space>
                    </Form.Slot>
                  </Col>
                  {!isEdit && (
                    <Col span={24}>
                      <Form.InputNumber
                        field='tokenCount'
                        label={t('新建数量')}
                        min={1}
                        extraText={t('批量创建时会在名称后自动添加随机后缀')}
                        rules={[
                          { required: true, message: t('请输入新建数量') },
                        ]}
                        style={{ width: '100%' }}
                      />
                    </Col>
                  )}
                </Row>
              </Card>

              {/* 额度设置 */}
              <Card className='!rounded-2xl shadow-sm border-0'>
                <div className='flex items-center mb-2'>
                  <Avatar size='small' color='green' className='mr-2 shadow-md'>
                    <IconCreditCard size={16} />
                  </Avatar>
                  <div>
                    <Text className='text-lg font-medium'>{t('额度设置')}</Text>
                    <div className='text-xs text-gray-600'>
                      {t('设置令牌可用额度和数量')}
                    </div>
                  </div>
                </div>
                <Row gutter={12}>
                  <Col span={24}>
                    <Form.InputNumber
                      field='remain_amount'
                      label={t('金额')}
                      prefix={getCurrencyConfig().symbol}
                      placeholder={t('输入金额')}
                      precision={6}
                      disabled={values.unlimited_quota}
                      min={0}
                      step={0.000001}
                      onChange={(val) => {
                        const amount = val === '' || val == null ? 0 : val;
                        formApiRef.current?.setValue('remain_amount', amount);
                        formApiRef.current?.setValue(
                          'remain_quota',
                          displayAmountToQuota(amount),
                        );
                      }}
                      style={{ width: '100%' }}
                      showClear
                    />
                  </Col>
                  <Col span={24}>
                    <div
                      className='text-xs cursor-pointer mt-1'
                      style={{ color: 'var(--semi-color-text-2)' }}
                      onClick={() => setShowQuotaInput((v) => !v)}
                    >
                      {showQuotaInput
                        ? `▾ ${t('收起原生额度输入')}`
                        : `▸ ${t('使用原生额度输入')}`}
                    </div>
                    <div
                      style={{ display: showQuotaInput ? 'block' : 'none' }}
                      className='mt-2'
                    >
                      <Form.InputNumber
                        field='remain_quota'
                        label={t('额度')}
                        placeholder={t('输入额度')}
                        disabled={values.unlimited_quota}
                        min={0}
                        step={500000}
                        rules={
                          values.unlimited_quota
                            ? []
                            : [{ required: true, message: t('请输入额度') }]
                        }
                        onChange={(val) => {
                          const quota = val === '' || val == null ? 0 : val;
                          formApiRef.current?.setValue('remain_quota', quota);
                          formApiRef.current?.setValue(
                            'remain_amount',
                            Number(quotaToDisplayAmount(quota).toFixed(6)),
                          );
                        }}
                        style={{ width: '100%' }}
                        showClear
                      />
                    </div>
                  </Col>
                  <Col span={24}>
                    <Form.Switch
                      field='unlimited_quota'
                      label={t('无限额度')}
                      size='default'
                      extraText={t(
                        '令牌的额度仅用于限制令牌本身的最大额度使用量，实际的使用受到账户的剩余额度限制',
                      )}
                    />
                  </Col>
                </Row>
              </Card>

              {/* 访问限制 */}
              <Card className='!rounded-2xl shadow-sm border-0'>
                <div className='flex items-center mb-2'>
                  <Avatar
                    size='small'
                    color='purple'
                    className='mr-2 shadow-md'
                  >
                    <IconLink size={16} />
                  </Avatar>
                  <div>
                    <Text className='text-lg font-medium'>{t('访问限制')}</Text>
                    <div className='text-xs text-gray-600'>
                      {t('设置令牌的访问限制')}
                    </div>
                  </div>
                </div>
                <Row gutter={12}>
                  <Col span={24}>
                    <Form.Select
                      field='model_limits'
                      label={t('模型限制列表')}
                      placeholder={t(
                        '请选择该令牌支持的模型，留空支持所有模型',
                      )}
                      multiple
                      optionList={models}
                      extraText={t('非必要，不建议启用模型限制')}
                      filter={selectFilter}
                      autoClearSearchValue={false}
                      searchPosition='dropdown'
                      showClear
                      style={{ width: '100%' }}
                    />
                  </Col>
                  <Col span={24}>
                    <Form.TextArea
                      field='allow_ips'
                      label={t('IP白名单（支持CIDR表达式）')}
                      placeholder={t('允许的IP，一行一个，不填写则不限制')}
                      autosize
                      rows={1}
                      extraText={t(
                        '请勿过度信任此功能，IP可能被伪造，请配合nginx和cdn等网关使用',
                      )}
                      showClear
                      style={{ width: '100%' }}
                    />
                  </Col>
                </Row>
              </Card>
            </div>
          )}
        </Form>
      </Spin>
    </SideSheet>
  );
};

export default EditTokenModal;
