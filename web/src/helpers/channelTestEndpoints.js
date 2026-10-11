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

// Shared by the test dialog and the request sender (including batch tests).
export const CHANNEL_TEST_ENDPOINTS = [
  { value: '', label: '', stream: true },
  {
    value: 'typesafe-systemone',
    label: 'TypeSafe (/v1/systemone)',
    stream: false,
    channels: [71],
  },
  {
    value: 'cohere-chat',
    label: 'Cohere Chat (/v2/chat)',
    stream: true,
    channels: [34],
  },
  {
    value: 'cohere-rerank',
    label: 'Cohere Rerank (/v2/rerank)',
    stream: false,
    channels: [34],
  },
  {
    value: 'cohere-embeddings',
    label: 'Cohere Embeddings (/v2/embed)',
    stream: false,
    channels: [34],
  },
  { value: 'openai', label: 'OpenAI (/v1/chat/completions)', stream: true },
  {
    value: 'openai-response',
    label: 'OpenAI Response (/v1/responses)',
    stream: true,
  },
  {
    value: 'openai-response-compact',
    label: 'OpenAI Response Compaction (/v1/responses/compact)',
    stream: false,
  },
  {
    value: 'openai-decisions',
    label: 'OpenAI Decisions (/v1/decisions)',
    stream: false,
    channels: [1],
  },
  { value: 'anthropic', label: 'Anthropic (/v1/messages)', stream: true },
  {
    value: 'gemini',
    label: 'Gemini (/v1beta/models/{model}:generateContent)',
    stream: true,
  },
  { value: 'jina-rerank', label: 'Jina Rerank (/v1/rerank)', stream: false },
  { value: 'image-generation', label: '', stream: false },
  { value: 'openai-video', label: 'OpenAI Video (/v1/videos)', stream: false },
  { value: 'embeddings', label: 'Embeddings (/v1/embeddings)', stream: false },
  {
    value: 'moderations',
    label: 'Moderations (/v1/moderations)',
    stream: false,
  },
  {
    value: 'audio-speech',
    label: 'Audio Speech (/v1/audio/speech)',
    stream: false,
  },
  {
    value: 'audio-transcription',
    label: 'Audio Transcription (/v1/audio/transcriptions)',
    stream: false,
  },
  {
    value: 'batch-generation',
    label: 'Batch Generation (/v1/batch/generations)',
    stream: false,
    channels: [67],
  },
];

export function getChannelTestEndpoints(channelType, t) {
  const type = Number(channelType);
  return CHANNEL_TEST_ENDPOINTS.filter((endpoint) => {
    if (endpoint.channels && !endpoint.channels.includes(type)) return false;
    if (type === 71) return ['', 'typesafe-systemone'].includes(endpoint.value);
    if (type === 72)
      return ['', 'openai', 'anthropic'].includes(endpoint.value);
    return true;
  }).map((endpoint) => ({
    ...endpoint,
    label:
      endpoint.value === ''
        ? t('自动检测')
        : endpoint.value === 'image-generation'
          ? t('图像生成') + ' (/v1/images/generations)'
          : endpoint.label,
  }));
}

export function channelTestSupportsStream(channelType, endpointType) {
  if (Number(channelType) === 71) return false;
  return getChannelTestEndpoints(channelType, (key) => key).some(
    (endpoint) => endpoint.value === endpointType && endpoint.stream,
  );
}
