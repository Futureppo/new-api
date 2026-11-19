import { API_ENDPOINTS } from '../constants/playground.constants';

const chatEndpoints = new Set([
  '',
  'openai',
  'openai-response',
  'anthropic',
  'gemini',
  'cohere-chat',
]);
export const isPlaygroundChat = (endpoint = '') => chatEndpoints.has(endpoint);
export const protocolURL = (endpoint) =>
  `${API_ENDPOINTS.PROTOCOL}/${encodeURIComponent(endpoint)}`;

export function protocolRequest(endpoint, group, model, stream, native, body) {
  return {
    url: protocolURL(endpoint),
    payload: { group, model, stream, native, body },
  };
}

export function playgroundTemplate(
  endpoint,
  stream = false,
  action = '',
  variant = '',
  family = '',
) {
  const messages = [{ role: 'user', content: 'Hello' }];
  const contents = [{ role: 'user', parts: [{ text: 'Hello' }] }];
  switch (endpoint) {
    case 'openai-response':
      return { input: messages, stream };
    case 'openai-response-compact':
      return { input: messages };
    case 'anthropic':
      return { messages, max_tokens: 256, stream };
    case 'gemini':
      return { contents };
    case 'vertex-native':
      if (action === 'predictLongRunning')
        return {
          instances: [{ prompt: 'A slow pan across a mountain' }],
          parameters: { sampleCount: 1 },
        };
      if (family === 'embedding' && action === 'predict')
        return { instances: [{ content: 'Hello' }] };
      if (family === 'multimodal_embedding')
        return {
          instances: [{ text: 'Hello' }],
          parameters: { dimension: 512 },
        };
      if (family === 'ocr')
        return {
          document: {
            type: 'document_url',
            document_url: 'https://example.com/document.pdf',
          },
        };
      if (family === 'fim')
        return {
          prompt: 'def greet(name):\n',
          suffix: '\nprint(greet("world"))',
          max_tokens: 128,
        };
      if (family === 'gemini_image')
        return {
          contents,
          generationConfig: { responseModalities: ['TEXT', 'IMAGE'] },
        };
      if (family === 'speech')
        return {
          contents,
          generationConfig: { responseModalities: ['AUDIO'] },
        };
      if (action === 'predict')
        return {
          instances: [{ prompt: 'A mountain at sunrise' }],
          parameters: { sampleCount: 1 },
        };
      if (action === 'embedContent') return { content: contents[0] };
      if (action === 'batchEmbedContents')
        return { requests: [{ content: contents[0] }] };
      if (action === 'rawPredict') return { messages, max_tokens: 256 };
      return { contents };
    case 'typesafe-systemone':
      return {
        state: 'Have a nice day.',
        questions: {
          greeting: { type: 'noul', instructions: 'Is this a greeting?' },
        },
      };
    case 'openai-decisions':
      return variant === 'named'
        ? {
            state: 'Hello',
            questions: {
              greeting: { type: 'noul', instructions: 'Is this a greeting?' },
            },
          }
        : {
            input: 'Hello',
            questions: [
              {
                type: 'predicate',
                name: 'greeting',
                instructions: 'Is this a greeting?',
              },
            ],
          };
    case 'image-generation':
      return { prompt: 'A watercolor of a mountain at sunrise' };
    case 'openai-video':
      return { prompt: 'A slow pan across a mountain landscape' };
    case 'embeddings':
      return { input: ['Hello', 'Good morning'] };
    case 'cohere-embeddings':
      return {
        texts: ['Hello', 'Good morning'],
        input_type: 'search_document',
        embedding_types: ['float'],
      };
    case 'jina-rerank':
    case 'cohere-rerank':
      return {
        query: 'What is an embedding?',
        documents: ['An embedding is a vector.', 'It is sunny today.'],
        top_n: 2,
      };
    case 'audio-speech':
      return { input: 'Hello', voice: 'alloy' };
    case 'audio-transcription':
      return { response_format: 'json' };
    case 'moderations':
      return { input: 'Hello' };
    case 'batch-generation':
      return {
        payload: {
          model: 'gemini-3-flash-preview',
          input_data: JSON.stringify({ request: { contents } }),
        },
      };
    default:
      return { messages, stream };
  }
}
