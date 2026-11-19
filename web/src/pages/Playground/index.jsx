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
  useContext,
  useEffect,
  useCallback,
  useMemo,
  useRef,
  useState,
} from 'react';
import { useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Layout, Toast, Modal } from '@douyinfe/semi-ui';

// Context
import { UserContext } from '../../context/User';
import { useIsMobile } from '../../hooks/common/useIsMobile';

// hooks
import { usePlaygroundState } from '../../hooks/playground/usePlaygroundState';
import { useMessageActions } from '../../hooks/playground/useMessageActions';
import { useApiRequest } from '../../hooks/playground/useApiRequest';
import { useSyncMessageAndCustomBody } from '../../hooks/playground/useSyncMessageAndCustomBody';
import { useMessageEdit } from '../../hooks/playground/useMessageEdit';
import { useDataLoader } from '../../hooks/playground/useDataLoader';

// Constants and utils
import {
  MESSAGE_ROLES,
  ERROR_MESSAGES,
  API_ENDPOINTS,
  MESSAGE_STATUS,
} from '../../constants/playground.constants';
import {
  getLogo,
  stringToColor,
  buildMessageContent,
  createMessage,
  createLoadingAssistantMessage,
  getTextContent,
  buildApiPayload,
  encodeToBase64,
  API,
} from '../../helpers';
import { getChannelTestEndpoints } from '../../helpers/channelTestEndpoints';
import {
  isPlaygroundChat,
  playgroundTemplate,
  protocolRequest,
} from '../../helpers/playgroundEndpoints';

// Components
import {
  OptimizedSettingsPanel,
  OptimizedDebugPanel,
  OptimizedMessageContent,
  OptimizedMessageActions,
} from '../../components/playground/OptimizedComponents';
import ChatArea from '../../components/playground/ChatArea';
import PlaygroundTool from '../../components/playground/PlaygroundTool';
import FloatingButtons from '../../components/playground/FloatingButtons';
import { PlaygroundProvider } from '../../contexts/PlaygroundContext';

// 生成头像
const generateAvatarDataUrl = (username) => {
  if (!username) {
    return 'https://lf3-static.bytednsdoc.com/obj/eden-cn/ptlz_zlp/ljhwZthlaukjlkulzlp/docs-icon.png';
  }
  const firstLetter = username[0].toUpperCase();
  const bgColor = stringToColor(username);
  const svg = `
    <svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 32 32">
      <circle cx="16" cy="16" r="16" fill="${bgColor}" />
      <text x="50%" y="50%" dominant-baseline="central" text-anchor="middle" font-size="16" fill="#ffffff" font-family="sans-serif">${firstLetter}</text>
    </svg>
  `;
  return `data:image/svg+xml;base64,${encodeToBase64(svg)}`;
};

const Playground = () => {
  const { t } = useTranslation();
  const [userState] = useContext(UserContext);
  const isMobile = useIsMobile();
  const styleState = { isMobile };
  const [searchParams] = useSearchParams();

  const state = usePlaygroundState();
  const {
    inputs,
    parameterEnabled,
    showDebugPanel,
    customRequestMode,
    customRequestBody,
    showSettings,
    models,
    groups,
    status,
    message,
    debugData,
    activeDebugTab,
    previewPayload,
    sseSourceRef,
    chatRef,
    handleInputChange,
    handleParameterToggle,
    debouncedSaveConfig,
    saveMessagesImmediately,
    handleConfigImport,
    handleConfigReset,
    setShowSettings,
    setModels,
    setGroups,
    setStatus,
    setMessage,
    setDebugData,
    setActiveDebugTab,
    setPreviewPayload,
    setShowDebugPanel,
    setCustomRequestMode,
    setCustomRequestBody,
  } = state;
  const [chatImages, setChatImages] = useState([]);
  const selectedEndpoint = inputs.endpoint || '';
  const isGenerating = message.some(
    (item) =>
      item.status === MESSAGE_STATUS.LOADING ||
      item.status === MESSAGE_STATUS.INCOMPLETE,
  );
  const messageRef = useRef(message);
  messageRef.current = message;
  const endpointKey = JSON.stringify([
    userState?.user?.id,
    inputs.group,
    inputs.model,
  ]);
  const [endpointResult, setEndpointResult] = useState(null);
  const endpointLoading = !!inputs.model && endpointResult?.key !== endpointKey;
  const endpoints =
    inputs.model && endpointResult?.key === endpointKey
      ? endpointResult.data
      : null;
  const endpoint =
    endpoints && !endpoints[selectedEndpoint]
      ? endpoints.openai
        ? ''
        : (Object.keys(endpoints)[0] ?? selectedEndpoint)
      : selectedEndpoint;
  const isChat = isPlaygroundChat(endpoint);
  const endpointOption = endpoints?.[endpoint || 'openai'];
  const endpointReady = !!endpointOption;
  const stream = inputs.stream && endpointOption?.stream !== false;

  useEffect(() => {
    if (!userState?.user || !inputs.model) return;
    let active = true;
    API.get(API_ENDPOINTS.ENDPOINTS, {
      params: { group: inputs.group, model: inputs.model },
      skipErrorHandler: true,
    }).then(
      ({ data }) => {
        if (active)
          setEndpointResult({
            key: endpointKey,
            data: data.success ? data.data : {},
          });
      },
      () => {
        if (active) setEndpointResult({ key: endpointKey, data: {} });
      },
    );
    return () => {
      active = false;
    };
  }, [endpointKey, userState?.user, inputs.group, inputs.model]);

  const endpointOptions = useMemo(
    () =>
      getChannelTestEndpoints(t).map((item) => {
        const option = endpoints?.[item.value || 'openai'];
        let label = item.label;
        if (item.value === 'image-generation') label = t('图像生成');
        if (item.value === 'jina-rerank') label = `${t('重排序')} (/v1/rerank)`;
        if (item.value === 'vertex-native' && option?.action)
          label = label.replace('{action}', option.action);
        return {
          ...item,
          label,
          disabled: !!item.value && !option,
          stream: option?.stream ?? item.stream,
        };
      }),
    [endpoints, t],
  );

  const nativeTemplate = useMemo(
    () =>
      playgroundTemplate(
        endpoint,
        !!stream,
        endpointOption?.action,
        endpointOption?.variant,
        endpointOption?.family,
      ),
    [
      endpoint,
      stream,
      endpointOption?.action,
      endpointOption?.variant,
      endpointOption?.family,
    ],
  );
  const savedDraft = inputs.nativeDrafts?.[endpoint];
  const nativeDraft = typeof savedDraft === 'string' ? savedDraft : undefined;
  const changeNativeDraft = useCallback(
    (draft) => {
      handleInputChange('nativeDrafts', {
        ...inputs.nativeDrafts,
        [endpoint]: draft,
      });
    },
    [endpoint, handleInputChange, inputs.nativeDrafts],
  );

  // API 请求相关
  const { sendRequest, onStopGenerator } = useApiRequest(
    setMessage,
    setDebugData,
    setActiveDebugTab,
    sseSourceRef,
    saveMessagesImmediately,
  );

  const sendChatRequest = useCallback(
    (body, isStream, id, native = false) => {
      if (!endpoint) return sendRequest(body, isStream, { id });
      if (!endpointReady || !isChat) return false;
      const { url, payload } = protocolRequest(
        endpoint,
        inputs.group,
        inputs.model,
        isStream,
        native,
        body,
      );
      return sendRequest(payload, isStream, { id, endpoint, url });
    },
    [endpoint, endpointReady, isChat, inputs.group, inputs.model, sendRequest],
  );

  function getCustomChatRequest() {
    const native = !!endpoint;
    const body = native
      ? nativeDraft?.trim()
        ? JSON.parse(nativeDraft)
        : nativeTemplate
      : JSON.parse(customRequestBody);
    if (!body || typeof body !== 'object' || Array.isArray(body)) {
      throw new Error(t('请求体必须是 JSON 对象'));
    }
    return {
      body,
      native,
      isStream: native
        ? endpoint === 'gemini'
          ? !!stream
          : body.stream === true
        : body.stream !== false,
    };
  }

  // 数据加载
  useDataLoader(userState, inputs, handleInputChange, setModels, setGroups);

  // 消息编辑
  const {
    editingMessageId,
    editValue,
    setEditValue,
    handleMessageEdit,
    handleEditSave,
    handleEditCancel,
  } = useMessageEdit(
    setMessage,
    inputs,
    parameterEnabled,
    (body, isStream, id) => {
      if (!endpointReady) {
        Toast.error(t('当前模型没有支持所选路径的可用渠道'));
        return false;
      }
      if (customRequestMode && (endpoint || customRequestBody)) {
        try {
          const request = getCustomChatRequest();
          return sendChatRequest(
            request.body,
            request.isStream,
            id,
            request.native,
          );
        } catch {
          Toast.error(t(ERROR_MESSAGES.JSON_PARSE_ERROR));
          return false;
        }
      }
      return sendChatRequest(
        body,
        isStream && endpointOption?.stream !== false,
        id,
      );
    },
    saveMessagesImmediately,
  );

  // 消息和自定义请求体同步
  const { syncMessageToCustomBody, syncCustomBodyToMessage } =
    useSyncMessageAndCustomBody(
      customRequestMode && !endpoint,
      customRequestBody,
      message,
      inputs,
      setCustomRequestBody,
      setMessage,
      debouncedSaveConfig,
    );

  // 角色信息
  const roleInfo = {
    user: {
      name: userState?.user?.username || 'User',
      avatar: generateAvatarDataUrl(userState?.user?.username),
    },
    assistant: {
      name: 'Assistant',
      avatar: getLogo(),
    },
    system: {
      name: 'System',
      avatar: getLogo(),
    },
  };

  // 消息操作
  const messageActions = useMessageActions(
    message,
    setMessage,
    onMessageSend,
    saveMessagesImmediately,
  );

  // 构建预览请求体
  const constructPreviewPayload = useCallback(() => {
    try {
      if (endpoint && (!isChat || customRequestMode)) {
        return nativeDraft?.trim() ? JSON.parse(nativeDraft) : nativeTemplate;
      }
      // 如果是自定义请求体模式且有自定义内容，直接返回解析后的自定义请求体
      if (customRequestMode && customRequestBody && customRequestBody.trim()) {
        try {
          return JSON.parse(customRequestBody);
        } catch (parseError) {
          console.warn('自定义请求体JSON解析失败，回退到默认预览:', parseError);
        }
      }

      // 默认预览逻辑
      let messages = [...message];

      // 如果存在用户消息
      if (
        !(
          messages.length === 0 ||
          messages.every((msg) => msg.role !== MESSAGE_ROLES.USER)
        )
      ) {
        // 处理最后一个用户消息的图片
        for (let i = messages.length - 1; i >= 0; i--) {
          if (messages[i].role === MESSAGE_ROLES.USER) {
            if (inputs.imageEnabled || chatImages.length) {
              const validImageUrls = [
                ...(inputs.imageEnabled
                  ? (inputs.imageUrls || []).filter((url) => url.trim() !== '')
                  : []),
                ...chatImages.map((image) => image.url),
              ];
              if (validImageUrls.length > 0) {
                const textContent = getTextContent(messages[i]) || '示例消息';
                const content = buildMessageContent(
                  textContent,
                  validImageUrls,
                  true,
                );
                messages[i] = { ...messages[i], content };
              }
            }
            break;
          }
        }
      }

      return buildApiPayload(messages, null, inputs, parameterEnabled);
    } catch (error) {
      console.error('构造预览请求体失败:', error);
      return null;
    }
  }, [
    inputs,
    parameterEnabled,
    message,
    customRequestMode,
    customRequestBody,
    endpoint,
    isChat,
    nativeDraft,
    nativeTemplate,
    chatImages,
  ]);

  // 发送消息
  function onMessageSend(content) {
    if (
      messageRef.current.some(
        (item) =>
          item.status === MESSAGE_STATUS.LOADING ||
          item.status === MESSAGE_STATUS.INCOMPLETE,
      )
    )
      return;
    if (!endpointReady) {
      Toast.error(t('当前模型没有支持所选路径的可用渠道'));
      return;
    }
    const loadingMessage = createLoadingAssistantMessage();
    const native = !!endpoint && customRequestMode;
    let body;
    let isStream = !!stream;
    let userContent = content;
    if (native || (customRequestMode && customRequestBody)) {
      try {
        ({ body, isStream } = getCustomChatRequest());
        if (native) userContent = t('自定义请求');
      } catch {
        Toast.error(t(ERROR_MESSAGES.JSON_PARSE_ERROR));
        return;
      }
    } else {
      userContent = buildMessageContent(
        content,
        [
          ...(inputs.imageEnabled
            ? (inputs.imageUrls || []).filter((url) => url.trim() !== '')
            : []),
          ...chatImages.map((image) => image.url),
        ],
        inputs.imageEnabled || chatImages.length > 0,
      );
    }
    const conversation = [
      ...messageRef.current,
      createMessage(MESSAGE_ROLES.USER, userContent),
    ];
    body ??= buildApiPayload(
      conversation,
      null,
      { ...inputs, stream: isStream },
      parameterEnabled,
    );
    const pending = [...conversation, loadingMessage];
    messageRef.current = pending;
    setMessage(pending);
    saveMessagesImmediately(pending);
    const sent = sendChatRequest(body, isStream, loadingMessage.id, native);
    if (sent && !customRequestMode) setChatImages([]);
    if (inputs.imageEnabled && !native)
      handleInputChange('imageEnabled', false);
  }

  // 切换推理展开状态
  const toggleReasoningExpansion = useCallback(
    (messageId) => {
      setMessage((prevMessages) =>
        prevMessages.map((msg) =>
          msg.id === messageId && msg.role === MESSAGE_ROLES.ASSISTANT
            ? { ...msg, isReasoningExpanded: !msg.isReasoningExpanded }
            : msg,
        ),
      );
    },
    [setMessage],
  );

  // 渲染函数
  const renderCustomChatContent = useCallback(
    ({ message, className }) => {
      const isCurrentlyEditing = editingMessageId === message.id;

      return (
        <OptimizedMessageContent
          message={message}
          className={className}
          styleState={styleState}
          onToggleReasoningExpansion={toggleReasoningExpansion}
          isEditing={isCurrentlyEditing}
          onEditSave={handleEditSave}
          onEditCancel={handleEditCancel}
          editValue={editValue}
          onEditValueChange={setEditValue}
        />
      );
    },
    [
      styleState,
      editingMessageId,
      editValue,
      handleEditSave,
      handleEditCancel,
      setEditValue,
      toggleReasoningExpansion,
    ],
  );

  const renderChatBoxAction = useCallback(
    (props) => {
      const { message: currentMessage } = props;
      const isAnyMessageGenerating = message.some(
        (msg) => msg.status === 'loading' || msg.status === 'incomplete',
      );
      const isCurrentlyEditing = editingMessageId === currentMessage.id;

      return (
        <OptimizedMessageActions
          message={currentMessage}
          styleState={styleState}
          onMessageReset={messageActions.handleMessageReset}
          onMessageCopy={messageActions.handleMessageCopy}
          onMessageDelete={messageActions.handleMessageDelete}
          onRoleToggle={messageActions.handleRoleToggle}
          onMessageEdit={handleMessageEdit}
          isAnyMessageGenerating={isAnyMessageGenerating}
          isEditing={isCurrentlyEditing}
        />
      );
    },
    [messageActions, styleState, message, editingMessageId, handleMessageEdit],
  );

  // Effects

  // 同步消息和自定义请求体
  useEffect(() => {
    syncMessageToCustomBody();
  }, [message, syncMessageToCustomBody]);

  useEffect(() => {
    syncCustomBodyToMessage();
  }, [customRequestBody, syncCustomBodyToMessage]);

  // 处理URL参数
  useEffect(() => {
    if (searchParams.get('expired')) {
      Toast.warning(t('登录过期，请重新登录！'));
    }
  }, [searchParams, t]);

  // Playground 组件无需再监听窗口变化，isMobile 由 useIsMobile Hook 自动更新

  // 构建预览payload
  useEffect(() => {
    const timer = setTimeout(() => {
      const preview = constructPreviewPayload();
      setPreviewPayload(preview);
      setDebugData((prev) => ({
        ...prev,
        previewRequest: preview ? JSON.stringify(preview, null, 2) : null,
        previewTimestamp: preview ? new Date().toISOString() : null,
      }));
    }, 300);

    return () => clearTimeout(timer);
  }, [
    message,
    inputs,
    parameterEnabled,
    customRequestMode,
    customRequestBody,
    constructPreviewPayload,
    setPreviewPayload,
    setDebugData,
  ]);

  // 自动保存配置
  useEffect(() => {
    debouncedSaveConfig();
  }, [
    inputs,
    parameterEnabled,
    showDebugPanel,
    customRequestMode,
    customRequestBody,
    debouncedSaveConfig,
  ]);

  // 清空对话的处理函数
  const handleClearMessages = useCallback(() => {
    setMessage([]);
    setChatImages([]);
    // 清空对话后保存，传入空数组
    setTimeout(() => saveMessagesImmediately([]), 0);
  }, [setMessage, saveMessagesImmediately]);

  const handlePasteImages = useCallback((images) => {
    setChatImages((previous) => [...previous, ...images]);
  }, []);

  // Playground Context 值
  const playgroundContextValue = {
    onPasteImages: handlePasteImages,
    onRemoveChatImage: (index) =>
      setChatImages((previous) => previous.filter((_, i) => i !== index)),
    chatImages,
    chatAttachmentsEnabled: !customRequestMode,
  };

  return (
    <PlaygroundProvider value={playgroundContextValue}>
      <div className='h-full'>
        <Layout className='h-full bg-transparent flex flex-col md:flex-row'>
          {(showSettings || !isMobile) && (
            <Layout.Sider
              className={`
              bg-transparent border-r-0 flex-shrink-0 overflow-auto mt-[60px]
              ${
                isMobile
                  ? 'fixed top-0 left-0 right-0 bottom-0 z-[1000] w-full h-auto bg-white shadow-lg'
                  : 'relative z-[1] w-80 h-[calc(100vh-66px)]'
              }
            `}
              width={isMobile ? '100%' : 320}
            >
              <OptimizedSettingsPanel
                inputs={inputs}
                parameterEnabled={parameterEnabled}
                models={models}
                groups={groups}
                endpoint={endpoint}
                endpointOptions={endpointOptions}
                endpointReady={endpointReady}
                endpointLoading={endpointLoading}
                isGenerating={isGenerating}
                nativeTemplate={nativeTemplate}
                onNativeDraftChange={changeNativeDraft}
                onSubmitNativeChat={() => {
                  onMessageSend(t('自定义请求'));
                  if (isMobile) setShowSettings(false);
                }}
                styleState={styleState}
                showSettings={showSettings}
                showDebugPanel={showDebugPanel}
                customRequestMode={customRequestMode}
                customRequestBody={customRequestBody}
                onInputChange={handleInputChange}
                onParameterToggle={handleParameterToggle}
                onCloseSettings={() => setShowSettings(false)}
                onConfigImport={handleConfigImport}
                onConfigReset={handleConfigReset}
                onCustomRequestModeChange={setCustomRequestMode}
                onCustomRequestBodyChange={setCustomRequestBody}
                previewPayload={previewPayload}
                messages={message}
              />
            </Layout.Sider>
          )}

          <Layout.Content className='relative flex-1 overflow-hidden'>
            <div className='overflow-hidden flex flex-col lg:flex-row h-[calc(100vh-66px)] mt-[60px]'>
              <div className='min-w-0 flex-1 flex flex-col'>
                {isChat ? (
                  <ChatArea
                    chatRef={chatRef}
                    message={message}
                    inputs={inputs}
                    styleState={styleState}
                    showDebugPanel={showDebugPanel}
                    roleInfo={roleInfo}
                    onMessageSend={onMessageSend}
                    onMessageCopy={messageActions.handleMessageCopy}
                    onMessageReset={messageActions.handleMessageReset}
                    onMessageDelete={messageActions.handleMessageDelete}
                    onStopGenerator={onStopGenerator}
                    onClearMessages={handleClearMessages}
                    onToggleDebugPanel={() =>
                      setShowDebugPanel(!showDebugPanel)
                    }
                    renderCustomChatContent={renderCustomChatContent}
                    renderChatBoxAction={renderChatBoxAction}
                  />
                ) : (
                  <PlaygroundTool
                    key={endpoint}
                    endpoint={endpoint}
                    option={endpointOption}
                    label={
                      endpointOptions.find((item) => item.value === endpoint)
                        ?.label
                    }
                    ready={endpointReady}
                    inputs={inputs}
                    draft={nativeDraft}
                    onDraftChange={changeNativeDraft}
                    setDebugData={setDebugData}
                    setActiveDebugTab={setActiveDebugTab}
                    onToggleDebugPanel={() =>
                      setShowDebugPanel(!showDebugPanel)
                    }
                    showDebugPanel={showDebugPanel}
                  />
                )}
              </div>

              {/* 调试面板 - 桌面端 */}
              {showDebugPanel && !isMobile && (
                <div className='w-96 flex-shrink-0 h-full'>
                  <OptimizedDebugPanel
                    debugData={debugData}
                    activeDebugTab={activeDebugTab}
                    onActiveDebugTabChange={setActiveDebugTab}
                    styleState={styleState}
                    customRequestMode={customRequestMode}
                  />
                </div>
              )}
            </div>

            {/* 调试面板 - 移动端覆盖层 */}
            {showDebugPanel && isMobile && (
              <div className='fixed top-0 left-0 right-0 bottom-0 z-[1000] bg-white overflow-auto shadow-lg'>
                <OptimizedDebugPanel
                  debugData={debugData}
                  activeDebugTab={activeDebugTab}
                  onActiveDebugTabChange={setActiveDebugTab}
                  styleState={styleState}
                  showDebugPanel={showDebugPanel}
                  onCloseDebugPanel={() => setShowDebugPanel(false)}
                  customRequestMode={customRequestMode}
                />
              </div>
            )}

            {/* 浮动按钮 */}
            <FloatingButtons
              styleState={styleState}
              showSettings={showSettings}
              showDebugPanel={showDebugPanel}
              onToggleSettings={() => setShowSettings(!showSettings)}
              onToggleDebugPanel={() => setShowDebugPanel(!showDebugPanel)}
            />
          </Layout.Content>
        </Layout>
      </div>
    </PlaygroundProvider>
  );
};

export default Playground;
