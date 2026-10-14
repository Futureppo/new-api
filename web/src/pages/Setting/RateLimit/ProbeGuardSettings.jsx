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

import React, { useEffect, useRef, useState } from 'react';
import {
  Banner,
  Button,
  Col,
  Form,
  InputNumber,
  Popconfirm,
  Row,
  Space,
  Spin,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import {
  API,
  compareObjects,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';

const prefix = 'probe_guard_setting.';
const defaults = {
  enabled: false,
  dry_run: true,
  probe_ping_enabled: true,
  batch_window_seconds: 30,
  batch_request_count: 3,
  text_rules:
    'Reply with exactly OK\nreply exactly: OK\nReply with exactly: OK\nsay OK\nREPLY `PONG` ONLY\n回复两个字：可用\n请回复"API 连接正常"，并告诉我你的模型名称。',
  first_user_ban_minutes: 10,
  second_user_ban_minutes: 60,
  first_ban_reason:
    '因测活请求，账号已暂停使用 {minutes} 分钟，将于 {until}（北京时间）恢复。请关闭客户端自动测活功能。',
  second_ban_reason:
    '再次检测到测活请求，账号已暂停使用 {minutes} 分钟，将于 {until}（北京时间）恢复。再次违规将永久封禁。',
  permanent_ban_reason:
    '因多次测活，账号已被永久封禁。如需申诉，请联系管理员。',
  whitelist_user_ids: '',
};

const booleanFields = new Set(['enabled', 'dry_run', 'probe_ping_enabled']);

export default function ProbeGuardSettings({ options, refresh }) {
  const { t } = useTranslation();
  const [inputs, setInputs] = useState(defaults);
  const [saved, setSaved] = useState(defaults);
  const [loading, setLoading] = useState(false);
  const formRef = useRef();
  const [testText, setTestText] = useState('');
  const [preview, setPreview] = useState(null);
  const [previewLoading, setPreviewLoading] = useState(false);
  const previewVersion = useRef(0);
  const [userId, setUserId] = useState(null);
  const [userState, setUserState] = useState(null);
  const [userLoading, setUserLoading] = useState(false);
  const userVersion = useRef(0);

  useEffect(() => {
    const values = { ...defaults };
    for (const key of Object.keys(defaults)) {
      if (options[prefix + key] !== undefined) {
        const value = options[prefix + key];
        values[key] = booleanFields.has(key)
          ? value === true || value === 'true'
          : value;
      }
    }
    setInputs(values);
    setSaved(values);
    formRef.current?.setValues(values);
    previewVersion.current++;
    setPreview(null);
  }, [options]);

  const change = (key) => (value) => {
    setInputs((previous) => ({ ...previous, [key]: value }));
    if (key === 'text_rules') {
      previewVersion.current++;
      setPreview(null);
    }
  };

  async function save() {
    if (
      !Number.isInteger(Number(inputs.batch_window_seconds)) ||
      Number(inputs.batch_window_seconds) < 1 ||
      Number(inputs.batch_window_seconds) > 3600 ||
      !Number.isInteger(Number(inputs.batch_request_count)) ||
      Number(inputs.batch_request_count) < 1 ||
      Number(inputs.batch_request_count) > 1000
    ) {
      return showError(
        t('统计窗口须为1至3600秒，测活请求阈值须为1至1000次，均为整数'),
      );
    }
    if (
      !Number.isInteger(Number(inputs.first_user_ban_minutes)) ||
      Number(inputs.first_user_ban_minutes) < 1 ||
      Number(inputs.first_user_ban_minutes) > 43200 ||
      !Number.isInteger(Number(inputs.second_user_ban_minutes)) ||
      Number(inputs.second_user_ban_minutes) < 1 ||
      Number(inputs.second_user_ban_minutes) > 43200
    ) {
      return showError(t('封禁时长必须为1至43200之间的整数分钟'));
    }
    const updates = compareObjects(inputs, saved);
    if (!updates.length) return showWarning(t('你似乎并没有修改什么'));
    setLoading(true);
    try {
      // Save rule and penalty settings before enabling enforcement.
      updates.sort(
        (a, b) => Number(a.key === 'enabled') - Number(b.key === 'enabled'),
      );
      for (const item of updates) {
        const response = await API.put('/api/option/', {
          key: prefix + item.key,
          value: String(inputs[item.key] ?? ''),
        });
        if (!response?.data?.success)
          throw new Error(response?.data?.message || t('部分保存失败，请重试'));
      }
      showSuccess(t('保存成功'));
      refresh();
    } catch (error) {
      showError(error.message || t('保存失败，请重试'));
    } finally {
      setLoading(false);
    }
  }

  async function testMatch() {
    const version = ++previewVersion.current;
    setPreviewLoading(true);
    try {
      const response = await API.post('/api/option/probe_guard/preview', {
        text: testText,
        rules: inputs.text_rules,
      });
      if (!response?.data?.success)
        throw new Error(response?.data?.message || t('操作失败'));
      if (previewVersion.current === version) setPreview(response.data.data);
    } catch (error) {
      showError(error.message);
    } finally {
      setPreviewLoading(false);
    }
  }

  async function loadUser(reset = false) {
    if (!Number.isInteger(Number(userId)) || Number(userId) <= 0)
      return showError(t('用户 ID 无效'));
    const version = ++userVersion.current;
    setUserLoading(true);
    try {
      const url = `/api/option/probe_guard/users/${userId}`;
      if (reset) {
        const response = await API.delete(url);
        if (!response?.data?.success)
          throw new Error(response?.data?.message || t('操作失败'));
        showSuccess(t('违规次数已重置，账号封禁状态不变'));
      }
      const response = await API.get(url);
      if (!response?.data?.success)
        throw new Error(response?.data?.message || t('操作失败'));
      if (userVersion.current === version) setUserState(response.data.data);
    } catch (error) {
      showError(error.message);
    } finally {
      setUserLoading(false);
    }
  }

  return (
    <Spin spinning={loading}>
      <Form
        initValues={defaults}
        getFormApi={(api) => {
          formRef.current = api;
        }}
      >
        <Form.Section text={t('单轮测活防护')}>
          <Typography.Paragraph type='tertiary'>
            {t(
              '仅检测单轮纯文本请求，允许携带系统提示和工具定义；多轮对话、会话续接及图片音频请求跳过检测。窗口内测活请求达到阈值后按账号处罚。',
            )}
          </Typography.Paragraph>
          <Row gutter={16}>
            <Col xs={24} md={8}>
              <Form.Switch
                field='enabled'
                label={t('启用单轮测活防护')}
                onChange={change('enabled')}
              />
            </Col>
            <Col xs={24} md={8}>
              <Form.Switch
                field='dry_run'
                label={t('试运行')}
                extraText={t(
                  '只记录达到阈值的批次，不封禁账号、不增加违规次数',
                )}
                onChange={change('dry_run')}
              />
            </Col>
            <Col xs={24} md={8}>
              <Form.Switch
                field='probe_ping_enabled'
                label={t('识别 probe_ping 工具探针')}
                extraText={t(
                  '同时校验独立确认文本、唯一工具、必填布尔参数和强制工具调用',
                )}
                onChange={change('probe_ping_enabled')}
              />
            </Col>
          </Row>
          <Row gutter={16}>
            <Col xs={24} md={12}>
              <Form.InputNumber
                field='batch_window_seconds'
                label={t('测活统计窗口（秒）')}
                min={1}
                max={3600}
                step={1}
                onChange={change('batch_window_seconds')}
              />
            </Col>
            <Col xs={24} md={12}>
              <Form.InputNumber
                field='batch_request_count'
                label={t('测活请求阈值（次）')}
                min={1}
                max={1000}
                step={1}
                onChange={change('batch_request_count')}
              />
            </Col>
          </Row>
          <Typography.Paragraph type='tertiary'>
            {t(
              '按账号统计滚动窗口内命中规则的请求次数，所有密钥合并，同一模型重复测活也逐次计数。未达到阈值正常放行；修改窗口、阈值或试运行状态后重新累计。',
            )}
          </Typography.Paragraph>
          <Form.TextArea
            field='text_rules'
            label={t('用户全文匹配列表')}
            autosize={{ minRows: 7, maxRows: 16 }}
            onChange={change('text_rules')}
            extraText={t(
              '每行一条，可新增、修改或删除；最多256条，每条1024字符。忽略大小写、全半角和连续空白，标点参与完整匹配；不支持正则。清空后仅保留已开启的工具探针规则。',
            )}
          />
          <Row gutter={16}>
            <Col xs={24} md={12}>
              <Form.InputNumber
                field='first_user_ban_minutes'
                label={t('第一次封禁账号')}
                min={1}
                max={43200}
                step={1}
                suffix={t('分钟')}
                onChange={change('first_user_ban_minutes')}
              />
            </Col>
            <Col xs={24} md={12}>
              <Form.InputNumber
                field='second_user_ban_minutes'
                label={t('第二次封禁账号')}
                min={1}
                max={43200}
                step={1}
                suffix={t('分钟')}
                onChange={change('second_user_ban_minutes')}
              />
            </Col>
          </Row>
          <Banner
            type='info'
            closeIcon={null}
            description={t(
              '每达到一次阈值记一次违规，第三次违规永久封禁。同批并发只处罚一次，封禁期间重试不追加处罚；解禁后重新累计测活请求。',
            )}
          />
          <Form.TextArea
            field='first_ban_reason'
            label={t('第一次封禁原因')}
            autosize={{ minRows: 2, maxRows: 5 }}
            onChange={change('first_ban_reason')}
          />
          <Form.TextArea
            field='second_ban_reason'
            label={t('第二次封禁原因')}
            autosize={{ minRows: 2, maxRows: 5 }}
            onChange={change('second_ban_reason')}
          />
          <Form.TextArea
            field='permanent_ban_reason'
            label={t('永久封禁原因')}
            autosize={{ minRows: 2, maxRows: 5 }}
            onChange={change('permanent_ban_reason')}
            extraText={t(
              '原因支持 {count}、{minutes}、{until}；解禁时间使用北京时间，展开后最多255字符。封禁后修改模板不改变已有封禁原因。',
            )}
          />
          <Form.TextArea
            field='whitelist_user_ids'
            label={t('用户白名单')}
            autosize={{ minRows: 2, maxRows: 5 }}
            onChange={change('whitelist_user_ids')}
            extraText={t(
              '输入用户 ID，支持逗号、空格或换行分隔；管理员默认跳过检测。',
            )}
          />
          <Button onClick={save} theme='solid'>
            {t('保存测活防护')}
          </Button>
        </Form.Section>
      </Form>
      <div className='mt-6 space-y-3'>
        <Typography.Title heading={6}>{t('全文匹配测试')}</Typography.Title>
        <Typography.Paragraph type='tertiary'>
          {t(
            '使用当前编辑的列表测试，不保存配置、不增加违规次数；此处仅测试文本匹配。',
          )}
        </Typography.Paragraph>
        <TextArea
          value={testText}
          aria-label={t('测试文本')}
          onChange={(value) => {
            setTestText(value);
            setPreview(null);
            previewVersion.current++;
          }}
          autosize={{ minRows: 2, maxRows: 6 }}
        />
        <Button onClick={testMatch} loading={previewLoading}>
          {t('测试匹配')}
        </Button>
        {preview && (
          <Banner
            type={preview.matched ? 'warning' : 'success'}
            closeIcon={null}
            description={
              preview.matched
                ? `${t('命中规则')}：${preview.match.rule}`
                : t('未命中规则')
            }
          />
        )}
      </div>
      <div className='mt-6 space-y-3'>
        <Typography.Title heading={6}>{t('账号违规记录')}</Typography.Title>
        <Space wrap>
          <InputNumber
            min={1}
            precision={0}
            value={userId}
            placeholder={t('用户 ID')}
            aria-label={t('用户 ID')}
            disabled={userLoading}
            onChange={(value) => {
              setUserId(value);
              setUserState(null);
              userVersion.current++;
            }}
          />
          <Button loading={userLoading} onClick={() => loadUser()}>
            {t('查询')}
          </Button>
          <Popconfirm
            title={t('重置该账号的测活违规次数？')}
            content={t(
              '清零违规次数和窗口内累计请求，不解除当前封禁。需要解禁时请在用户管理中操作。',
            )}
            onConfirm={() => loadUser(true)}
          >
            <Button type='warning' disabled={!userState || userLoading}>
              {t('重置违规次数')}
            </Button>
          </Popconfirm>
        </Space>
        {userState && (
          <div className='space-y-1 text-sm'>
            <div>
              {t('累计违规次数')}：{userState.state.offense_count}
            </div>
            <div>
              {t('账号状态')}：
              {userState.status === 2 ? t('已禁用') : t('已启用')}
            </div>
            {userState.disable_reason && (
              <div>
                {t('封禁原因')}：{userState.disable_reason}
              </div>
            )}
            {userState.state.last_offense_at > 0 && (
              <div>
                {t('最近触发时间')}：
                {new Date(
                  userState.state.last_offense_at * 1000,
                ).toLocaleString('zh-CN', {
                  timeZone: 'Asia/Shanghai',
                  hour12: false,
                })}{' '}
                (UTC+8)
              </div>
            )}
            {userState.state.last_rule_id && (
              <div>
                {t('命中规则')}：{userState.state.last_rule_id}
              </div>
            )}
            {userState.state.last_request_id && (
              <div>Request ID：{userState.state.last_request_id}</div>
            )}
          </div>
        )}
      </div>
    </Spin>
  );
}
