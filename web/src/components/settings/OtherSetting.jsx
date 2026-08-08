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

import React, { useContext, useEffect, useRef, useState } from 'react';
import {
  Banner,
  Button,
  Col,
  Form,
  Row,
  Modal,
  Space,
  Card,
} from '@douyinfe/semi-ui';
import { API, showError, showSuccess, timestamp2string } from '../../helpers';
import { useTranslation } from 'react-i18next';
import { StatusContext } from '../../context/Status';
import Text from '@douyinfe/semi-ui/lib/es/typography/text';
import SiteBackgroundSetting from './SiteBackgroundSetting';
import {
  DEFAULT_SITE_BACKGROUND_CONFIG,
  SITE_BACKGROUND_OPTION_KEY,
} from '../../services/siteBackground';

const LEGAL_USER_AGREEMENT_KEY = 'legal.user_agreement';
const LEGAL_PRIVACY_POLICY_KEY = 'legal.privacy_policy';
const UPDATE_REPOSITORY = 'Futureppo/new-api';
const UPDATE_BRANCH = 'main';
const GITHUB_API_BASE_URL = `https://api.github.com/repos/${UPDATE_REPOSITORY}`;
const GITHUB_REPOSITORY_URL = `https://github.com/${UPDATE_REPOSITORY}`;
const GITHUB_COMMITS_URL = `${GITHUB_API_BASE_URL}/commits?sha=${UPDATE_BRANCH}&per_page=5`;
const COMMIT_HASH_PATTERN = /\b[0-9a-f]{7,40}\b/gi;

const escapeHtml = (value = '') =>
  String(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');

const fetchGitHubJson = async (url) => {
  const response = await fetch(url, {
    headers: {
      Accept: 'application/vnd.github+json',
    },
  });

  if (!response.ok) {
    const error = new Error(`GitHub API request failed: ${response.status}`);
    error.status = response.status;
    throw error;
  }

  return response.json();
};

const extractCommitHash = (version = '') => {
  const matches = String(version).match(COMMIT_HASH_PATTERN);
  return matches?.length ? matches[matches.length - 1] : '';
};

const getCompareUrl = (localCommit) =>
  `${GITHUB_API_BASE_URL}/compare/${encodeURIComponent(
    localCommit,
  )}...${encodeURIComponent(UPDATE_BRANCH)}`;

const getCommitUrl = (sha) =>
  `${GITHUB_REPOSITORY_URL}/commit/${encodeURIComponent(sha)}`;

const renderCommitList = (commits) => {
  const items = commits
    .map((item) => {
      const sha = item.sha?.slice(0, 7) || 'unknown';
      const message =
        item.commit?.message?.split('\n')[0] || 'No commit message';
      const author =
        item.commit?.author?.name || item.author?.login || 'unknown';
      const date = item.commit?.author?.date
        ? new Date(item.commit.author.date).toLocaleString()
        : '';
      const url = item.html_url || getCommitUrl(item.sha);

      return `<li><a href="${escapeHtml(url)}" target="_blank" rel="noreferrer">${escapeHtml(
        sha,
      )}</a> ${escapeHtml(date)} ${escapeHtml(author)} - ${escapeHtml(message)}</li>`;
    })
    .join('');

  return `<ul>${items}</ul>`;
};

const renderUpdateContent = ({
  currentVersion,
  localCommit,
  commits,
  notice = '',
  fallback = false,
}) => {
  const compareSummary = fallback
    ? `无法精确比较当前版本，以下为 ${escapeHtml(
        UPDATE_REPOSITORY,
      )} ${escapeHtml(UPDATE_BRANCH)} 分支最新提交：`
    : `以下为远端 ${escapeHtml(UPDATE_BRANCH)} 分支相对本地版本新增的提交：`;

  return `
    ${notice ? `<p>${escapeHtml(notice)}</p>` : ''}
    <p>当前版本：${escapeHtml(currentVersion || '未知')}</p>
    <p>本地 commit：${escapeHtml(localCommit || '无法识别')}</p>
    <p>远端分支：${escapeHtml(UPDATE_REPOSITORY)} ${escapeHtml(
      UPDATE_BRANCH,
    )}</p>
    <p>${compareSummary}</p>
    ${renderCommitList(commits)}
  `;
};

const renderFallbackCommitUpdates = (currentVersion, localCommit, commits) =>
  renderUpdateContent({
    currentVersion,
    localCommit,
    commits,
    fallback: true,
  });

const getFallbackDetailUrl = () =>
  `${GITHUB_REPOSITORY_URL}/commits/${encodeURIComponent(UPDATE_BRANCH)}`;

const getFallbackTitle = () => `无法精确比较：${UPDATE_BRANCH} 最新提交`;

const fetchFallbackCommits = async () => {
  const commits = await fetchGitHubJson(GITHUB_COMMITS_URL);
  if (!Array.isArray(commits) || commits.length === 0) {
    throw new Error('No commits found for update check');
  }
  return commits;
};

const renderCompareUpdates = (currentVersion, localCommit, compareResult) => {
  const commits = Array.isArray(compareResult.commits)
    ? compareResult.commits
    : [];
  const notice =
    compareResult.status === 'diverged'
      ? '本地版本与远端 main 已分叉，下面列出远端 main 相对本地缺失的新提交。'
      : '';

  return renderUpdateContent({
    currentVersion,
    localCommit,
    commits,
    notice,
  });
};

const getCompareTitle = (compareResult) => {
  const aheadBy = Number(compareResult.ahead_by || 0);
  const statusText =
    compareResult.status === 'diverged' ? 'main 已分叉并领先' : 'main 领先';
  return `发现新版本：${statusText} ${aheadBy} 个提交`;
};

const getCompareDetailUrl = (localCommit) =>
  `${GITHUB_REPOSITORY_URL}/compare/${encodeURIComponent(
    localCommit,
  )}...${encodeURIComponent(UPDATE_BRANCH)}`;

const showFallbackUpdateModal = async ({
  currentVersion,
  localCommit,
  setUpdateData,
  setShowUpdateModal,
}) => {
  const commits = await fetchFallbackCommits();
  const title = getFallbackTitle();
  setUpdateData({
    tag_name: UPDATE_BRANCH,
    title,
    content: renderFallbackCommitUpdates(currentVersion, localCommit, commits),
    detailUrl: getFallbackDetailUrl(),
  });
  setShowUpdateModal(true);
};

const renderLatestCommitSummary = (commit) => {
  const sha = commit.sha?.slice(0, 7) || UPDATE_BRANCH;
  const message = commit.commit?.message?.split('\n')[0] || 'No commit message';
  return `${sha} ${message}`;
};

const getLatestCommitSuccessText = (compareResult) => {
  const latestCommit =
    compareResult.base_commit || compareResult.merge_base_commit;
  if (!latestCommit?.sha) {
    return `已是最新版本：${UPDATE_BRANCH}`;
  }
  return `已是最新版本：${renderLatestCommitSummary(latestCommit)}`;
};

const getAheadBy = (compareResult) => Number(compareResult?.ahead_by || 0);

const isRemoteAhead = (compareResult) => getAheadBy(compareResult) > 0;

const isIdentical = (compareResult) =>
  getAheadBy(compareResult) === 0 && compareResult?.status === 'identical';

const isLocalAhead = (compareResult) =>
  getAheadBy(compareResult) === 0 && compareResult?.status === 'behind';

const getLocalAheadMessage = (compareResult) => {
  const behindBy = Number(compareResult?.behind_by || 0);
  return behindBy > 0
    ? `当前本地版本已领先远端 main ${behindBy} 个提交`
    : '当前本地版本已领先远端 main';
};

const getNoRemoteCommitMessage = (compareResult) =>
  compareResult?.status === 'diverged'
    ? '本地版本与远端 main 已分叉，但远端 main 暂无相对本地的新提交'
    : '远端 main 暂无相对本地的新提交';

const OtherSetting = () => {
  const { t } = useTranslation();
  let [inputs, setInputs] = useState({
    Notice: '',
    [LEGAL_USER_AGREEMENT_KEY]: '',
    [LEGAL_PRIVACY_POLICY_KEY]: '',
    SystemName: '',
    Logo: '',
    [SITE_BACKGROUND_OPTION_KEY]: JSON.stringify(
      DEFAULT_SITE_BACKGROUND_CONFIG,
    ),
    Footer: '',
    About: '',
    HomePageContent: '',
  });
  let [loading, setLoading] = useState(false);
  const [showUpdateModal, setShowUpdateModal] = useState(false);
  const [statusState, statusDispatch] = useContext(StatusContext);
  const [updateData, setUpdateData] = useState({
    tag_name: '',
    title: '',
    content: '',
    detailUrl: '',
  });

  const updateOption = async (key, value) => {
    setLoading(true);
    const res = await API.put('/api/option/', {
      key,
      value,
    });
    const { success, message } = res.data;
    if (success) {
      setInputs((inputs) => ({ ...inputs, [key]: value }));
    } else {
      showError(message);
    }
    setLoading(false);
  };

  const [loadingInput, setLoadingInput] = useState({
    Notice: false,
    [LEGAL_USER_AGREEMENT_KEY]: false,
    [LEGAL_PRIVACY_POLICY_KEY]: false,
    SystemName: false,
    Logo: false,
    HomePageContent: false,
    About: false,
    Footer: false,
    CheckUpdate: false,
  });
  const handleInputChange = async (value, e) => {
    const name = e.target.id;
    setInputs((inputs) => ({ ...inputs, [name]: value }));
  };

  // 通用设置
  const formAPISettingGeneral = useRef();
  // 通用设置 - Notice
  const submitNotice = async () => {
    try {
      setLoadingInput((loadingInput) => ({ ...loadingInput, Notice: true }));
      await updateOption('Notice', inputs.Notice);
      showSuccess(t('公告已更新'));
    } catch (error) {
      console.error(t('公告更新失败'), error);
      showError(t('公告更新失败'));
    } finally {
      setLoadingInput((loadingInput) => ({ ...loadingInput, Notice: false }));
    }
  };
  // 通用设置 - UserAgreement
  const submitUserAgreement = async () => {
    try {
      setLoadingInput((loadingInput) => ({
        ...loadingInput,
        [LEGAL_USER_AGREEMENT_KEY]: true,
      }));
      await updateOption(
        LEGAL_USER_AGREEMENT_KEY,
        inputs[LEGAL_USER_AGREEMENT_KEY],
      );
      showSuccess(t('用户协议已更新'));
    } catch (error) {
      console.error(t('用户协议更新失败'), error);
      showError(t('用户协议更新失败'));
    } finally {
      setLoadingInput((loadingInput) => ({
        ...loadingInput,
        [LEGAL_USER_AGREEMENT_KEY]: false,
      }));
    }
  };
  // 通用设置 - PrivacyPolicy
  const submitPrivacyPolicy = async () => {
    try {
      setLoadingInput((loadingInput) => ({
        ...loadingInput,
        [LEGAL_PRIVACY_POLICY_KEY]: true,
      }));
      await updateOption(
        LEGAL_PRIVACY_POLICY_KEY,
        inputs[LEGAL_PRIVACY_POLICY_KEY],
      );
      showSuccess(t('隐私政策已更新'));
    } catch (error) {
      console.error(t('隐私政策更新失败'), error);
      showError(t('隐私政策更新失败'));
    } finally {
      setLoadingInput((loadingInput) => ({
        ...loadingInput,
        [LEGAL_PRIVACY_POLICY_KEY]: false,
      }));
    }
  };
  // 个性化设置
  const formAPIPersonalization = useRef();
  //  个性化设置 - SystemName
  const submitSystemName = async () => {
    try {
      setLoadingInput((loadingInput) => ({
        ...loadingInput,
        SystemName: true,
      }));
      await updateOption('SystemName', inputs.SystemName);
      showSuccess(t('系统名称已更新'));
    } catch (error) {
      console.error(t('系统名称更新失败'), error);
      showError(t('系统名称更新失败'));
    } finally {
      setLoadingInput((loadingInput) => ({
        ...loadingInput,
        SystemName: false,
      }));
    }
  };

  // 个性化设置 - Logo
  const submitLogo = async () => {
    try {
      setLoadingInput((loadingInput) => ({ ...loadingInput, Logo: true }));
      await updateOption('Logo', inputs.Logo);
      showSuccess('Logo 已更新');
    } catch (error) {
      console.error('Logo 更新失败', error);
      showError('Logo 更新失败');
    } finally {
      setLoadingInput((loadingInput) => ({ ...loadingInput, Logo: false }));
    }
  };
  // 个性化设置 - 首页内容
  const submitOption = async (key) => {
    try {
      setLoadingInput((loadingInput) => ({
        ...loadingInput,
        HomePageContent: true,
      }));
      await updateOption(key, inputs[key]);
      showSuccess('首页内容已更新');
    } catch (error) {
      console.error('首页内容更新失败', error);
      showError('首页内容更新失败');
    } finally {
      setLoadingInput((loadingInput) => ({
        ...loadingInput,
        HomePageContent: false,
      }));
    }
  };
  // 个性化设置 - 关于
  const submitAbout = async () => {
    try {
      setLoadingInput((loadingInput) => ({ ...loadingInput, About: true }));
      await updateOption('About', inputs.About);
      showSuccess('关于内容已更新');
    } catch (error) {
      console.error('关于内容更新失败', error);
      showError('关于内容更新失败');
    } finally {
      setLoadingInput((loadingInput) => ({ ...loadingInput, About: false }));
    }
  };
  // 个性化设置 - 页脚
  const submitFooter = async () => {
    try {
      setLoadingInput((loadingInput) => ({ ...loadingInput, Footer: true }));
      await updateOption('Footer', inputs.Footer);
      showSuccess('页脚内容已更新');
    } catch (error) {
      console.error('页脚内容更新失败', error);
      showError('页脚内容更新失败');
    } finally {
      setLoadingInput((loadingInput) => ({ ...loadingInput, Footer: false }));
    }
  };

  const checkUpdate = async () => {
    try {
      setLoadingInput((loadingInput) => ({
        ...loadingInput,
        CheckUpdate: true,
      }));

      const currentVersion = statusState?.status?.version || '';
      const localCommit = extractCommitHash(currentVersion);
      if (!localCommit) {
        await showFallbackUpdateModal({
          currentVersion,
          localCommit,
          setUpdateData,
          setShowUpdateModal,
        });
        return;
      }

      let compareResult;
      try {
        compareResult = await fetchGitHubJson(getCompareUrl(localCommit));
      } catch (error) {
        if (error.status === 404) {
          await showFallbackUpdateModal({
            currentVersion,
            localCommit,
            setUpdateData,
            setShowUpdateModal,
          });
          return;
        }
        throw error;
      }

      if (isRemoteAhead(compareResult)) {
        setUpdateData({
          tag_name: UPDATE_BRANCH,
          title: getCompareTitle(compareResult),
          content: renderCompareUpdates(
            currentVersion,
            localCommit,
            compareResult,
          ),
          detailUrl: compareResult.html_url || getCompareDetailUrl(localCommit),
        });
        setShowUpdateModal(true);
      } else if (isIdentical(compareResult)) {
        showSuccess(getLatestCommitSuccessText(compareResult));
      } else if (isLocalAhead(compareResult)) {
        showSuccess(getLocalAheadMessage(compareResult));
      } else {
        showSuccess(getNoRemoteCommitMessage(compareResult));
      }
    } catch (error) {
      console.error('Failed to check for updates:', error);
      showError('检查更新失败，请稍后再试');
    } finally {
      setLoadingInput((loadingInput) => ({
        ...loadingInput,
        CheckUpdate: false,
      }));
    }
  };
  const getOptions = async () => {
    const res = await API.get('/api/option/');
    const { success, message, data } = res.data;
    if (success) {
      let newInputs = {};
      data.forEach((item) => {
        if (item.key in inputs) {
          newInputs[item.key] = item.value;
        }
      });
      setInputs(newInputs);
      formAPISettingGeneral.current.setValues(newInputs);
      formAPIPersonalization.current.setValues(newInputs);
    } else {
      showError(message);
    }
  };

  useEffect(() => {
    getOptions();
  }, []);

  // Function to open GitHub update detail page
  const openGitHubRelease = () => {
    window.open(updateData.detailUrl || GITHUB_REPOSITORY_URL, '_blank');
  };

  const getStartTimeString = () => {
    const timestamp = statusState?.status?.start_time;
    return statusState.status ? timestamp2string(timestamp) : '';
  };

  return (
    <Row>
      <Col
        span={24}
        style={{
          marginTop: '10px',
          display: 'flex',
          flexDirection: 'column',
          gap: '10px',
        }}
      >
        {/* 版本信息 */}
        <Form>
          <Card>
            <Form.Section text={t('系统信息')}>
              <Row>
                <Col span={16}>
                  <Space>
                    <Text>
                      {t('当前版本')}：
                      {statusState?.status?.version || t('未知')}
                    </Text>
                    <Button
                      type='primary'
                      onClick={checkUpdate}
                      loading={loadingInput['CheckUpdate']}
                    >
                      {t('检查更新')}
                    </Button>
                  </Space>
                </Col>
              </Row>
              <Row>
                <Col span={16}>
                  <Text>
                    {t('启动时间')}：{getStartTimeString()}
                  </Text>
                </Col>
              </Row>
            </Form.Section>
          </Card>
        </Form>
        {/* 通用设置 */}
        <Form
          values={inputs}
          getFormApi={(formAPI) => (formAPISettingGeneral.current = formAPI)}
        >
          <Card>
            <Form.Section text={t('通用设置')}>
              <Form.TextArea
                label={t('公告')}
                placeholder={t(
                  '在此输入新的公告内容，支持 Markdown & HTML 代码',
                )}
                field={'Notice'}
                onChange={handleInputChange}
                style={{ fontFamily: 'JetBrains Mono, Consolas' }}
                autosize={{ minRows: 6, maxRows: 12 }}
              />
              <Button onClick={submitNotice} loading={loadingInput['Notice']}>
                {t('设置公告')}
              </Button>
              <Form.TextArea
                label={t('用户协议')}
                placeholder={t(
                  '在此输入用户协议内容，支持 Markdown & HTML 代码',
                )}
                field={LEGAL_USER_AGREEMENT_KEY}
                onChange={handleInputChange}
                style={{ fontFamily: 'JetBrains Mono, Consolas' }}
                autosize={{ minRows: 6, maxRows: 12 }}
                helpText={t(
                  '填写用户协议内容后，用户注册时将被要求勾选已阅读用户协议',
                )}
              />
              <Button
                onClick={submitUserAgreement}
                loading={loadingInput[LEGAL_USER_AGREEMENT_KEY]}
              >
                {t('设置用户协议')}
              </Button>
              <Form.TextArea
                label={t('隐私政策')}
                placeholder={t(
                  '在此输入隐私政策内容，支持 Markdown & HTML 代码',
                )}
                field={LEGAL_PRIVACY_POLICY_KEY}
                onChange={handleInputChange}
                style={{ fontFamily: 'JetBrains Mono, Consolas' }}
                autosize={{ minRows: 6, maxRows: 12 }}
                helpText={t(
                  '填写隐私政策内容后，用户注册时将被要求勾选已阅读隐私政策',
                )}
              />
              <Button
                onClick={submitPrivacyPolicy}
                loading={loadingInput[LEGAL_PRIVACY_POLICY_KEY]}
              >
                {t('设置隐私政策')}
              </Button>
            </Form.Section>
          </Card>
        </Form>
        {/* 个性化设置 */}
        <Form
          values={inputs}
          getFormApi={(formAPI) => (formAPIPersonalization.current = formAPI)}
        >
          <Card>
            <Form.Section text={t('个性化设置')}>
              <Form.Input
                label={t('系统名称')}
                placeholder={t('在此输入系统名称')}
                field={'SystemName'}
                onChange={handleInputChange}
              />
              <Button
                onClick={submitSystemName}
                loading={loadingInput['SystemName']}
              >
                {t('设置系统名称')}
              </Button>
              <Form.Input
                label={t('Logo 图片地址')}
                placeholder={t('在此输入 Logo 图片地址')}
                field={'Logo'}
                onChange={handleInputChange}
              />
              <Button onClick={submitLogo} loading={loadingInput['Logo']}>
                {t('设置 Logo')}
              </Button>
              <SiteBackgroundSetting
                value={inputs[SITE_BACKGROUND_OPTION_KEY]}
                onSaved={(value) =>
                  setInputs((current) => ({
                    ...current,
                    [SITE_BACKGROUND_OPTION_KEY]: value,
                  }))
                }
              />
              <Form.TextArea
                label={t('首页内容')}
                placeholder={t(
                  '在此输入首页内容，支持 Markdown & HTML 代码，设置后首页的状态信息将不再显示。如果输入的是一个链接，则会使用该链接作为 iframe 的 src 属性，这允许你设置任意网页作为首页',
                )}
                field={'HomePageContent'}
                onChange={handleInputChange}
                style={{ fontFamily: 'JetBrains Mono, Consolas' }}
                autosize={{ minRows: 6, maxRows: 12 }}
              />
              <Button
                onClick={() => submitOption('HomePageContent')}
                loading={loadingInput['HomePageContent']}
              >
                {t('设置首页内容')}
              </Button>
              <Form.TextArea
                label={t('关于')}
                placeholder={t(
                  '在此输入新的关于内容，支持 Markdown & HTML 代码。如果输入的是一个链接，则会使用该链接作为 iframe 的 src 属性，这允许你设置任意网页作为关于页面',
                )}
                field={'About'}
                onChange={handleInputChange}
                style={{ fontFamily: 'JetBrains Mono, Consolas' }}
                autosize={{ minRows: 6, maxRows: 12 }}
              />
              <Button onClick={submitAbout} loading={loadingInput['About']}>
                {t('设置关于')}
              </Button>
              {/*  */}
              <Banner
                fullMode={false}
                type='info'
                description={t(
                  '移除 One API 的版权标识必须首先获得授权，项目维护需要花费大量精力，如果本项目对你有意义，请主动支持本项目',
                )}
                closeIcon={null}
                style={{ marginTop: 15 }}
              />
              <Form.Input
                label={t('页脚')}
                placeholder={t(
                  '在此输入新的页脚，留空则使用默认页脚，支持 HTML 代码',
                )}
                field={'Footer'}
                onChange={handleInputChange}
              />
              <Button onClick={submitFooter} loading={loadingInput['Footer']}>
                {t('设置页脚')}
              </Button>
            </Form.Section>
          </Card>
        </Form>
      </Col>
      <Modal
        title={updateData.title || t('新版本') + '：' + updateData.tag_name}
        visible={showUpdateModal}
        onCancel={() => setShowUpdateModal(false)}
        footer={[
          <Button
            key='details'
            type='primary'
            onClick={() => {
              setShowUpdateModal(false);
              openGitHubRelease();
            }}
          >
            {t('详情')}
          </Button>,
        ]}
      >
        <div dangerouslySetInnerHTML={{ __html: updateData.content }}></div>
      </Modal>
    </Row>
  );
};

export default OtherSetting;
