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

import { afterEach, beforeEach, expect, mock, spyOn, test } from 'bun:test';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import i18next from 'i18next';
import { I18nextProvider } from 'react-i18next';
import { SSE } from 'sse.js';
import { MESSAGE_STATUS } from '../../constants/playground.constants';

// Bun shares module mocks between test files by default. Keep this browser
// harness in its own process so pricing/UI tests can use their own mocks.
if (process.env.NEW_API_PLAYGROUND_SSE_TEST === '1') {
  mock.module('../../helpers', () => ({
    getUserIdFromLocalStorage: () => '1',
    handleApiError: (error, response) => ({
      error: error.message,
      status: response?.status,
    }),
    processThinkTags: (content, reasoningContent) => ({
      content,
      reasoningContent,
    }),
    processIncompleteThinkTags: (content, reasoningContent) => ({
      content,
      reasoningContent,
    }),
    copy: async () => {},
  }));

  // Keep React rendering real while replacing browser-only UI components.
  const Container = ({ children }) =>
    React.createElement('div', null, children);
  const Panel = ({ header, children }) =>
    React.createElement('section', null, header, children);
  mock.module('@douyinfe/semi-ui', () => ({
    Button: Container,
    Tooltip: Container,
    Toast: {},
    Collapse: Object.assign(Container, { Panel }),
    Badge: ({ count }) =>
      React.createElement('span', { 'data-count': String(count) }, count),
    Typography: { Text: Container },
  }));

  const { useApiRequest } = await import('./useApiRequest.jsx');
  const { default: SSEViewer } = await import(
    '../../components/playground/SSEViewer.jsx'
  );
  const i18n = i18next.createInstance();
  await i18n.init({ lng: 'zh', resources: { zh: { translation: {} } } });
  const render = (element) =>
    renderToStaticMarkup(
      React.createElement(I18nextProvider, { i18n }, element),
    );

  class MockXMLHttpRequest extends EventTarget {
    static HEADERS_RECEIVED = 2;
    static latest;
    static sendError;
    status = 200;
    responseText = '';
    response = '';
    abortCount = 0;

    constructor() {
      super();
      MockXMLHttpRequest.latest = this;
    }

    open(method, url) {
      this.method = method;
      this.url = url;
    }

    setRequestHeader() {}

    getAllResponseHeaders() {
      return 'content-type: text/event-stream';
    }

    send(payload) {
      this.payload = payload;
      if (MockXMLHttpRequest.sendError) throw MockXMLHttpRequest.sendError;
      this.readyState = MockXMLHttpRequest.HEADERS_RECEIVED;
      this.dispatchEvent(new Event('readystatechange'));
    }

    abort() {
      this.abortCount++;
      this.dispatchEvent(new Event('abort'));
    }

    receive(text) {
      this.responseText += text;
      this.response = this.responseText;
      this.dispatchEvent(new Event('progress'));
    }

    finish() {
      this.dispatchEvent(new Event('load'));
    }
  }

  const originalXMLHttpRequest = globalThis.XMLHttpRequest;
  let consoleError;
  beforeEach(() => {
    globalThis.XMLHttpRequest = MockXMLHttpRequest;
    MockXMLHttpRequest.sendError = null;
    consoleError = spyOn(console, 'error').mockImplementation(() => {});
  });
  afterEach(async () => {
    await Bun.sleep(5);
    globalThis.XMLHttpRequest = originalXMLHttpRequest;
    consoleError.mockRestore();
  });

  function startRequest() {
    const state = {
      messages: [
        {
          role: 'assistant',
          status: MESSAGE_STATUS.LOADING,
          content: '',
          reasoningContent: '',
        },
      ],
      debug: {},
      tab: null,
    };
    const sourceRef = { current: null };
    const saveMessages = mock(() => {});
    let api;
    function Harness() {
      api = useApiRequest(
        (update) => {
          state.messages = update(state.messages);
        },
        (update) => {
          state.debug = update(state.debug);
        },
        (tab) => {
          state.tab = tab;
        },
        sourceRef,
        saveMessages,
      );
      return null;
    }
    render(React.createElement(Harness));
    api.sendRequest(
      { model: 'stealth/crouton-alpha', stream: true, messages: [] },
      true,
    );
    return {
      state,
      sourceRef,
      saveMessages,
      api,
      xhr: MockXMLHttpRequest.latest,
    };
  }

  const frame = (data) => `data: ${JSON.stringify(data)}\n\n`;
  const textFrame = (content) => frame({ choices: [{ delta: { content } }] });
  const reasoningFrame = frame({
    choices: [
      {
        delta: {
          reasoning: 'thinking',
          reasoning_details: [
            { type: 'reasoning.summary', summary: 'thinking' },
          ],
        },
      },
    ],
  });
  const usageFrame = frame({
    choices: [
      { delta: { content: '', role: 'assistant' }, finish_reason: 'stop' },
    ],
    usage: { prompt_tokens: 3, completion_tokens: 2, total_tokens: 5 },
  });
  const doneFrame = 'data: [DONE]\n\n';

  test('three OpenRouter keep-alives allow reasoning, content and final usage to complete', async () => {
    const { state, sourceRef, saveMessages, xhr } = startRequest();
    expect(sourceRef.current).toBeInstanceOf(SSE);
    expect(xhr.url).toBe('/pg/chat/completions');
    xhr.receive(': OPENROUTER PROCESSING\n\n'.repeat(3));
    expect(state.messages[0].status).toBe(MESSAGE_STATUS.LOADING);
    expect(state.debug.sseMessages).toEqual([]);
    expect(state.debug.isStreaming).toBe(true);
    expect(state.tab).toBe('request');

    xhr.receive(
      reasoningFrame + textFrame('你好') + ': OPENROUTER PROCESSING\n\n',
    );
    expect(state.messages[0].content).toBe('你好');
    expect(state.messages[0].reasoningContent).toBe('thinking');
    xhr.receive(
      frame({ choices: [{ delta: {}, finish_reason: 'stop' }] }) + usageFrame,
    );
    expect(state.debug.isStreaming).toBe(true);
    xhr.receive(doneFrame + textFrame('late content'));
    xhr.finish();

    expect(state.messages[0].status).toBe(MESSAGE_STATUS.COMPLETE);
    expect(state.messages[0].content).toBe('你好');
    expect(state.debug.sseMessages).toHaveLength(5);
    expect(JSON.parse(state.debug.sseMessages[3]).usage.total_tokens).toBe(5);
    expect(state.debug.sseMessages[4]).toBe('[DONE]');
    expect(state.debug.isStreaming).toBe(false);
    expect(sourceRef.current).toBeNull();
    expect(xhr.abortCount).toBe(1);
    expect(consoleError).not.toHaveBeenCalled();
    await Bun.sleep(5);
    expect(saveMessages).toHaveBeenCalledTimes(1);
    expect(saveMessages.mock.calls[0][0][0].content).toBe('你好');
  });

  test('empty data and fragmented CRLF multi-line events do not trigger parsing failures', () => {
    const { state, xhr } = startRequest();
    const stream =
      ': OPENROUTER PROCESSING\r\n\r\n' +
      'data:\r\n\r\ndata:   \r\n\r\n' +
      'data: {"choices":\r\ndata: [{"delta":{"content":"分段输出"}}]}\r\n\r\n' +
      ': keep-alive\r\n\r\ndata: [DONE]\r\n\r\n';
    for (const character of stream) xhr.receive(character);
    expect(state.messages[0].content).toBe('分段输出');
    expect(state.messages[0].status).toBe(MESSAGE_STATUS.COMPLETE);
    expect(state.debug.sseMessages).toHaveLength(2);
    expect(consoleError).not.toHaveBeenCalled();
  });

  for (const prefix of ['', 'partial answer']) {
    test(`an upstream error ends the stream once with preceding content '${prefix}'`, async () => {
      const { state, sourceRef, saveMessages, xhr } = startRequest();
      if (prefix) xhr.receive(reasoningFrame + textFrame(prefix));
      const errorFrame = frame({
        error: {
          code: 'server_error',
          message: 'Provider disconnected unexpectedly',
        },
        choices: [{ delta: { content: '' }, finish_reason: 'error' }],
      });
      xhr.receive(errorFrame + textFrame('ignored') + doneFrame);
      xhr.finish();
      expect(state.messages[0].status).toBe(MESSAGE_STATUS.ERROR);
      expect(state.messages[0].errorCode).toBe('server_error');
      expect(state.messages[0].content).toBe(
        (prefix ? prefix + '\n\n' : '') + 'Provider disconnected unexpectedly',
      );
      if (prefix) expect(state.messages[0].reasoningContent).toBe('thinking');
      expect(state.debug.sseMessages).toHaveLength(prefix ? 3 : 1);
      expect(state.debug.response).toContain(
        'Provider disconnected unexpectedly',
      );
      expect(state.debug.isStreaming).toBe(false);
      expect(sourceRef.current).toBeNull();
      expect(xhr.abortCount).toBe(1);
      await Bun.sleep(5);
      expect(saveMessages).toHaveBeenCalledTimes(1);
      expect(saveMessages.mock.calls[0][0][0].status).toBe(
        MESSAGE_STATUS.ERROR,
      );
    });
  }

  test('malformed JSON is recorded once and preserves preceding content', async () => {
    const { state, sourceRef, saveMessages, xhr } = startRequest();
    xhr.receive(textFrame('partial') + 'data: {"choices":\n\n' + doneFrame);
    expect(state.messages[0].status).toBe(MESSAGE_STATUS.ERROR);
    expect(state.messages[0].content).toStartWith(
      'partial\n\n解析响应数据时发生错误',
    );
    expect(state.debug.sseMessages).toHaveLength(2);
    expect(state.debug.sseMessages[1]).toBe('{"choices":');
    expect(state.debug.isStreaming).toBe(false);
    expect(sourceRef.current).toBeNull();
    expect(xhr.abortCount).toBe(1);
    await Bun.sleep(5);
    expect(saveMessages).toHaveBeenCalledTimes(1);
  });

  test('HTTP errors preserve the provider message, code and HTTP status', () => {
    const { state, sourceRef, xhr } = startRequest();
    xhr.status = 429;
    xhr.receive('{"error":{"code":429,"message":"Rate limited"}}');
    expect(state.messages[0].content).toBe('Rate limited');
    expect(state.messages[0].errorCode).toBe(429);
    expect(state.messages[0].status).toBe(MESSAGE_STATUS.ERROR);
    expect(state.debug.response).toContain('"status": 429');
    expect(state.debug.isStreaming).toBe(false);
    expect(sourceRef.current).toBeNull();
    expect(xhr.abortCount).toBe(1);
  });

  test('EOF without DONE ends loading with an error and retains partial content', () => {
    const { state, sourceRef, xhr } = startRequest();
    xhr.receive(textFrame('partial'));
    xhr.finish();
    expect(state.messages[0].content).toBe('partial\n\n连接已断开');
    expect(state.messages[0].status).toBe(MESSAGE_STATUS.ERROR);
    expect(state.debug.isStreaming).toBe(false);
    expect(sourceRef.current).toBeNull();
  });

  test('manual cancellation keeps the existing completion behavior and ends loading', async () => {
    const { state, sourceRef, saveMessages, api, xhr } = startRequest();
    xhr.receive(reasoningFrame + textFrame('partial'));
    api.onStopGenerator();
    xhr.finish();
    expect(state.messages[0].content).toBe('partial');
    expect(state.messages[0].reasoningContent).toBe('thinking');
    expect(state.messages[0].status).toBe(MESSAGE_STATUS.COMPLETE);
    expect(state.debug.isStreaming).toBe(false);
    expect(sourceRef.current).toBeNull();
    expect(xhr.abortCount).toBe(1);
    await Bun.sleep(5);
    expect(saveMessages).toHaveBeenCalledTimes(1);
  });

  test('synchronous startup failures are handled after listeners are attached', () => {
    MockXMLHttpRequest.sendError = new Error('Network unavailable');
    const { state, sourceRef } = startRequest();
    expect(state.messages[0].content).toContain('建立连接时发生错误');
    expect(state.messages[0].content).toContain('Network unavailable');
    expect(state.messages[0].status).toBe(MESSAGE_STATUS.ERROR);
    expect(state.debug.isStreaming).toBe(false);
    expect(sourceRef.current).toBeNull();
  });

  test('the SSE viewer excludes empty messages from event and error counts', () => {
    const html = render(
      React.createElement(SSEViewer, {
        sseData: ['', ' \n\t', '{"choices":[]}', ' [DONE] '],
      }),
    );
    expect(html).toContain('data-count="2"');
    expect(html).not.toContain('解析错误');
    expect(html).not.toContain('错误</span>');
    expect(
      render(React.createElement(SSEViewer, { sseData: ['', ' '] })),
    ).toContain('暂无SSE响应数据');

    const malformed = render(
      React.createElement(SSEViewer, { sseData: ['', '{'] }),
    );
    expect(malformed).toContain('解析错误');
    expect(malformed).toContain('data-count="1 错误"');
  });
} else {
  test('playground SSE regression suite with isolated browser mocks', async () => {
    const child = Bun.spawn([process.execPath, 'test', import.meta.path], {
      env: { ...process.env, NEW_API_PLAYGROUND_SSE_TEST: '1' },
      stdout: 'pipe',
      stderr: 'pipe',
    });
    const [exitCode, stdout, stderr] = await Promise.all([
      child.exited,
      new Response(child.stdout).text(),
      new Response(child.stderr).text(),
    ]);
    if (exitCode !== 0) throw new Error(stdout + stderr);
    expect(exitCode).toBe(0);
  });
}
