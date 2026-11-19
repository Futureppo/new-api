import React, { useEffect, useRef, useState } from 'react';
import {
  Button,
  Card,
  Input,
  InputNumber,
  Radio,
  RadioGroup,
  Select,
  Switch,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import {
  API_ENDPOINTS,
  DEBUG_TABS,
} from '../../constants/playground.constants';
import { getUserIdFromLocalStorage } from '../../helpers';
import {
  playgroundTemplate,
  protocolRequest,
} from '../../helpers/playgroundEndpoints';
import { playgroundReply } from '../../helpers/playgroundResponse';
import {
  clipboardImages,
  readImageDataURL,
} from '../../helpers/playgroundImages';
import PlaygroundImagePicker from './PlaygroundImagePicker';

const controls = {
  'image-generation': [
    ['prompt', '提示词', 'textarea', true],
    ['size', '尺寸'],
    ['n', '生成数量', 'number'],
  ],
  'openai-video': [
    ['prompt', '提示词', 'textarea', true],
    ['size', '尺寸'],
    ['seconds', '时长（秒）', 'number'],
    ['input_reference', '参考图片', 'file'],
  ],
  embeddings: [
    ['input', '输入文本（每行一条）', 'lines', true],
    ['dimensions', '维度', 'number'],
  ],
  'cohere-embeddings': [
    ['input', '输入文本（每行一条）', 'lines', true],
    [
      'input_type',
      '输入用途',
      ['search_document', 'search_query', 'classification', 'clustering'],
    ],
    ['dimensions', '维度', 'number'],
  ],
  'jina-rerank': [
    ['query', '查询', 'textarea', true],
    ['documents', '候选文档（每行一条）', 'lines', true],
    ['top_n', '返回条数', 'number'],
  ],
  'cohere-rerank': [
    ['query', '查询', 'textarea', true],
    ['documents', '候选文档（每行一条）', 'lines', true],
    ['top_n', '返回条数', 'number'],
  ],
  moderations: [['input', '待审核文本', 'textarea', true]],
  'audio-speech': [
    ['input', '输入文本', 'textarea', true],
    ['voice', '音色'],
  ],
  'audio-transcription': [
    ['file', '音频文件', 'file', true],
    ['language', '语言'],
    ['prompt', '提示词'],
  ],
};

function safeMediaURL(value) {
  if (typeof value !== 'string') return '';
  return /^(https?:\/\/|blob:|\/v1\/videos\/)/i.test(value) ||
    /^data:(image\/(png|jpeg|webp|gif)|audio\/[\w.+-]+|video\/mp4);base64,/i.test(
      value,
    )
    ? value
    : '';
}

function videoResultURL(value, taskId) {
  if (!taskId || !/^https?:\/\//i.test(value)) return value;
  try {
    const url = new URL(value);
    if (url.pathname === `/v1/videos/${encodeURIComponent(taskId)}/content`)
      return url.pathname + url.search;
  } catch {
    return '';
  }
  return value;
}

function MediaResult({ url, type, label }) {
  const src = safeMediaURL(url);
  const { t } = useTranslation();
  if (!src) return null;
  return (
    <div className='space-y-2'>
      {type === 'image' && (
        <img src={src} alt={label} className='max-h-96 max-w-full rounded-lg' />
      )}
      {type === 'video' && (
        <video
          controls
          preload='metadata'
          src={src}
          className='max-h-96 max-w-full rounded-lg'
        />
      )}
      {type === 'audio' && (
        <audio controls preload='metadata' src={src} className='max-w-full' />
      )}
      <a
        href={src}
        download
        target='_blank'
        rel='noreferrer'
        className='text-[var(--semi-color-primary)]'
      >
        {t('下载')}
      </a>
    </div>
  );
}

function ToolResult({ data, endpoint, family, documents }) {
  const { t } = useTranslation();
  if (data == null) return null;
  const record =
    data.code === 'success' || data.success === true ? data.data : data;
  if (typeof record === 'string')
    return (
      <Typography.Paragraph className='whitespace-pre-wrap'>
        {record}
      </Typography.Paragraph>
    );
  if (!record) return null;
  const taskData =
    record.data && !Array.isArray(record.data) ? record.data : record;
  const nativeContent = endpoint === 'vertex-native';
  const type =
    endpoint === 'image-generation' ||
    ['imagen', 'gemini_image'].includes(family)
      ? 'image'
      : endpoint === 'openai-video' || family === 'video'
        ? 'video'
        : endpoint === 'audio-speech' ||
            family === 'speech' ||
            family === 'music'
          ? 'audio'
          : null;
  const inlineMedia = (record.candidates?.[0]?.content?.parts || [])
    .map((part) => part.inlineData)
    .filter((part) => part?.mimeType?.startsWith(`${type}/`) && part.data)
    .map((part) => `data:${part.mimeType};base64,${part.data}`);
  const media = [
    ...(Array.isArray(record.data) && endpoint === 'image-generation'
      ? record.data
      : []),
    ...(Array.isArray(taskData.images) ? taskData.images : []),
    ...(Array.isArray(taskData.unsigned_urls)
      ? taskData.unsigned_urls.map((url) => ({ url }))
      : []),
    ...(Array.isArray(taskData.artifacts) ? taskData.artifacts : []),
    ...(Array.isArray(taskData.videos) ? taskData.videos : []),
    ...(nativeContent && type && Array.isArray(record.predictions)
      ? record.predictions
      : []),
    ...(Array.isArray(taskData.outcome?.media_urls)
      ? taskData.outcome.media_urls
      : []),
  ];
  const urls = [
    ...new Set(
      [
        record.result_url,
        taskData.result_url,
        taskData.url,
        taskData.video?.url,
        taskData.content?.video_url,
        ...inlineMedia,
        ...media.map((entry) =>
          typeof entry === 'string'
            ? entry
            : entry?.url ||
              entry?.video_url ||
              (entry?.b64_json
                ? `data:image/png;base64,${entry.b64_json}`
                : entry?.bytesBase64Encoded && type === 'image'
                  ? `data:${entry.mimeType || 'image/png'};base64,${entry.bytesBase64Encoded}`
                  : null),
        ),
      ]
        .filter(safeMediaURL)
        .map((url) =>
          type === 'video' ? videoResultURL(url, record.task_id) : url,
        )
        .filter(Boolean),
    ),
  ];
  const vectors = endpoint.includes('embeddings')
    ? (record.embeddings?.float ?? record.data?.map?.((item) => item.embedding))
    : nativeContent && ['embedding', 'multimodal_embedding'].includes(family)
      ? record.embedding?.values
        ? [record.embedding.values]
        : record.predictions?.flatMap((item) =>
            [
              item.embeddings?.values,
              item.textEmbedding,
              item.imageEmbedding,
            ].filter(Array.isArray),
          )
      : null;
  const ranking =
    endpoint.includes('rerank') && Array.isArray(record.results)
      ? record.results
      : null;
  const moderation =
    endpoint === 'moderations' && Array.isArray(record.results)
      ? record.results
      : null;
  const answers =
    record.answers && typeof record.answers === 'object'
      ? Object.entries(record.answers)
      : [];
  const transcript =
    record.text || (nativeContent && playgroundReply(record).content);
  const taskStatus =
    record.status ||
    (family === 'video' && record.done === false ? 'queued' : '');
  return (
    <div className='space-y-3'>
      {taskStatus && (
        <Typography.Text>
          {t('任务状态')} {taskStatus}
          {record.progress != null &&
            ` · ${typeof record.progress === 'number' ? `${record.progress}%` : record.progress}`}
        </Typography.Text>
      )}
      {record.fail_reason && (
        <Typography.Paragraph type='danger'>
          {record.fail_reason}
        </Typography.Paragraph>
      )}
      {type &&
        urls.map((url, index) => (
          <MediaResult
            key={index}
            url={url}
            type={type}
            label={`${t('结果')} ${index + 1}`}
          />
        ))}
      {endpoint === 'batch-generation' &&
        urls.map((url, index) => (
          <div key={index}>
            <a href={url} target='_blank' rel='noreferrer' download>
              {t('下载结果')}
            </a>
          </div>
        ))}
      {typeof transcript === 'string' && !!transcript && (
        <Typography.Paragraph className='whitespace-pre-wrap'>
          {transcript}
        </Typography.Paragraph>
      )}
      {Array.isArray(vectors) && (
        <div className='flex flex-wrap items-center gap-2'>
          <Typography.Text>
            {t('向量数量')} {vectors.length}
            {Array.isArray(vectors[0]) &&
              ` · ${t('维度')} ${vectors[0].length}`}
          </Typography.Text>
          <Button
            size='small'
            onClick={() =>
              navigator.clipboard.writeText(JSON.stringify(vectors))
            }
          >
            {t('复制向量')}
          </Button>
        </div>
      )}
      {ranking && (
        <table className='w-full text-left text-sm'>
          <thead>
            <tr>
              <th>{t('文档编号')}</th>
              <th>{t('文档')}</th>
              <th>{t('得分')}</th>
            </tr>
          </thead>
          <tbody>
            {[...ranking]
              .sort(
                (a, b) =>
                  (b.relevance_score ?? b.score ?? 0) -
                  (a.relevance_score ?? a.score ?? 0),
              )
              .map((row) => (
                <tr
                  key={row.index}
                  className='border-t border-[var(--semi-color-border)]'
                >
                  <td className='p-2'>{row.index + 1}</td>
                  <td className='p-2 break-words'>
                    {typeof row.document === 'string'
                      ? row.document
                      : typeof row.document?.text === 'string'
                        ? row.document.text
                        : documents?.[row.index]}
                  </td>
                  <td className='p-2 tabular-nums'>
                    {(row.relevance_score ?? row.score)?.toFixed?.(4)}
                  </td>
                </tr>
              ))}
          </tbody>
        </table>
      )}
      {moderation?.map((result, index) => (
        <div key={index} className='text-sm'>
          <strong>{result.flagged ? t('已标记') : t('未标记')}</strong>
          <ul className='my-1 space-y-1'>
            {Object.entries(result.category_scores || {}).map(
              ([name, score]) => (
                <li key={name}>
                  {name} {Number(score).toFixed(4)}
                  {result.categories?.[name] ? ' ✓' : ''}
                </li>
              ),
            )}
          </ul>
        </div>
      ))}
      {answers.length > 0 && (
        <dl className='space-y-2 text-sm'>
          {answers.map(([key, answer]) => (
            <div
              key={key}
              className='rounded border border-[var(--semi-color-border)] p-2'
            >
              <dt className='font-medium'>{answer?.name ?? key}</dt>
              <dd className='m-0 break-all font-mono'>
                {JSON.stringify(answer)}
              </dd>
            </div>
          ))}
        </dl>
      )}
      <details
        open={
          (!type || urls.length === 0) &&
          !transcript &&
          !Array.isArray(vectors) &&
          !ranking &&
          !moderation &&
          !taskStatus &&
          !answers.length
        }
      >
        <summary className='cursor-pointer text-sm'>{t('原始响应')}</summary>
        <pre className='max-h-80 overflow-auto whitespace-pre-wrap break-all text-xs'>
          {JSON.stringify(data, null, 2)}
        </pre>
      </details>
    </div>
  );
}

export default function PlaygroundTool({
  endpoint,
  label,
  option,
  inputs,
  ready,
  draft,
  onDraftChange,
  setDebugData,
  setActiveDebugTab,
  onToggleDebugPanel,
  showDebugPanel,
}) {
  const { t } = useTranslation();
  const fields = option?.audio_formats?.length
    ? [
        ...controls[endpoint],
        ['response_format', '输出格式', option.audio_formats],
      ]
    : controls[endpoint];
  const [native, setNative] = useState(!fields);
  const [values, setValues] = useState({});
  const [file, setFile] = useState(null);
  const [imageEdit, setImageEdit] = useState(false);
  const [images, setImages] = useState([]);
  const [queryImages, setQueryImages] = useState([]);
  const [embeddingMode, setEmbeddingMode] = useState('');
  const [videoMode, setVideoMode] = useState('');
  const nextImageId = useRef(0);
  const [response, setResponse] = useState(null);
  const [documents, setDocuments] = useState([]);
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [querying, setQuerying] = useState(false);
  const [audio, setAudio] = useState('');
  const latestAudio = useRef('');
  const mounted = useRef(true);
  const taskKey = `playground-task-${getUserIdFromLocalStorage()}-${endpoint}`;
  const [taskId, setTaskId] = useState(
    () => localStorage.getItem(taskKey) || '',
  );
  const audioUpload = endpoint === 'audio-transcription';
  const imageTool = endpoint === 'image-generation';
  const videoTool = endpoint === 'openai-video';
  const rerankTool = endpoint === 'jina-rerank' || endpoint === 'cohere-rerank';
  const moderationTool = endpoint === 'moderations';
  const embeddingTool =
    endpoint === 'embeddings' || endpoint === 'cohere-embeddings';
  const embeddingUpload = embeddingTool && !native && option?.embedding_image;
  const inputUpload =
    option?.input_image && (!native || endpoint === 'vertex-native');
  const videoModes = [
    option?.video_frames_limit && 'frames',
    option?.video_references_limit && 'reference',
  ].filter(Boolean);
  const videoJSONUpload = videoTool && !native && videoModes.length > 0;
  const selectedVideoMode = videoModes.includes(videoMode)
    ? videoMode
    : videoModes.length === 1
      ? videoModes[0]
      : '';
  const videoUpload =
    videoTool && (videoJSONUpload ? option?.video_image_data : option?.upload);
  const imageUpload = imageTool && option?.image_edit && option.upload;
  const fileField = imageTool
    ? imageEdit && imageUpload && 'image'
    : !videoJSONUpload &&
      option?.upload &&
      fields?.find((field) => field[2] === 'file')?.[0];
  const showReference =
    (imageEdit && imageUpload) || embeddingUpload || inputUpload || videoUpload;
  const referenceImages = showReference ? images : [];
  const videoLimit =
    selectedVideoMode === 'frames'
      ? option?.video_frames_limit
      : selectedVideoMode === 'reference'
        ? option?.video_references_limit
        : Math.max(
            option?.video_frames_limit || 0,
            option?.video_references_limit || 0,
          );
  const embeddingFiles = embeddingUpload ? referenceImages : [];
  const inputFiles = inputUpload ? referenceImages : [];
  const queryFile =
    !native && option?.image_query ? queryImages[0]?.file : null;
  const embeddingModes = [
    option?.embedding_separate && 'separate',
    option?.embedding_joint && 'joint',
  ].filter(Boolean);
  const chooseEmbeddingMode = embeddingModes.length > 1;
  const selectedEmbeddingMode = embeddingModes.includes(embeddingMode)
    ? embeddingMode
    : embeddingModes.length === 1
      ? embeddingModes[0]
      : '';
  const imageLimit = embeddingUpload
    ? selectedEmbeddingMode === 'joint'
      ? option.embedding_joint_limit
      : option.embedding_limit
    : inputUpload
      ? option.input_image_limit
      : videoJSONUpload
        ? videoLimit
        : option?.upload_limit;
  const referenceLabel = t(
    embeddingTool
      ? '嵌入图片'
      : moderationTool
        ? '审核图片'
        : rerankTool
          ? '候选图片'
          : '参考图片',
  );
  const textOptional = (field) =>
    (field === 'input' &&
      (embeddingFiles.length > 0 ||
        (moderationTool && inputFiles.length > 0))) ||
    (rerankTool &&
      ((field === 'query' && !!queryFile) ||
        (field === 'documents' && inputFiles.length > 0)));
  const draftText =
    draft ??
    JSON.stringify(
      playgroundTemplate(
        endpoint,
        false,
        option?.action,
        option?.variant,
        option?.family,
      ),
      null,
      2,
    );
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      if (latestAudio.current) URL.revokeObjectURL(latestAudio.current);
    };
  }, []);
  const addImages = (files, target = 'reference') => {
    if (!files.length) return;
    const entries = files.map((file) => ({ file, id: nextImageId.current++ }));
    if (target === 'query') {
      setQueryImages(entries);
    } else {
      setImages((previous) =>
        imageLimit === 1 && entries.length === 1
          ? entries
          : [...previous, ...entries],
      );
    }
    setError('');
  };
  const pasteReference = (event) => {
    const files = clipboardImages(event.clipboardData);
    if (!files.length) return;
    event.preventDefault();
    if (!imageTool) {
      if (embeddingUpload || inputUpload || videoUpload) {
        const target = event.target.closest('[data-image-target]')?.dataset
          .imageTarget;
        addImages(files, target);
      } else
        setError(
          t(
            embeddingTool
              ? '当前模型没有支持图片嵌入的可用渠道'
              : videoTool && option?.video_image_url
                ? '当前模型仅支持参考图链接'
                : '当前模型没有支持这种图片输入的可用渠道',
          ),
        );
      return;
    }
    if (!imageUpload) {
      setError(
        t(
          option?.image_edit
            ? '当前模型仅支持参考图链接'
            : '当前模型不支持图生图',
        ),
      );
      return;
    }
    setImageEdit(true);
    addImages(files);
  };

  const headers = () => ({ 'New-Api-User': getUserIdFromLocalStorage() });
  const decode = async (res) => {
    const contentType = res.headers.get('content-type') || '';
    if (
      res.ok &&
      (contentType.startsWith('audio/') ||
        (endpoint === 'audio-speech' &&
          contentType.startsWith('application/octet-stream')))
    )
      return res.blob();
    const text = await res.text();
    try {
      return JSON.parse(text);
    } catch {
      return text;
    }
  };
  const checkResponse = (res, data) => {
    if (!res.ok || data?.error || data?.success === false) {
      throw new Error(
        data?.error?.message ||
          data?.message ||
          (typeof data === 'string' && data) ||
          t('请求发生错误'),
      );
    }
  };
  const submit = async (event) => {
    event.preventDefault();
    if (!ready || submitting || querying) return;
    setError('');
    try {
      if (
        embeddingFiles.length &&
        chooseEmbeddingMode &&
        !selectedEmbeddingMode
      )
        throw new Error(t('请选择向量生成方式'));
      if (imageLimit > 0 && referenceImages.length > imageLimit)
        throw new Error(
          t('当前模型最多支持 {{limit}} 张图片', { limit: imageLimit }),
        );
      if (queryFile && queryImages.length > 1)
        throw new Error(t('重排序只能添加一张查询图片'));
      let body;
      if (native) {
        body = JSON.parse(draftText);
        if (!body || typeof body !== 'object' || Array.isArray(body))
          throw new Error(t('请求体必须是 JSON 对象'));
      } else {
        body = {};
        for (const [key, , kind, required] of fields) {
          if (kind === 'file' || (key === 'query' && queryFile)) continue;
          const value =
            key === 'voice'
              ? values.voice || option?.default_voice
              : key === 'response_format' && !kind.includes(values[key])
                ? undefined
                : values[key];
          if (required && !textOptional(key) && !String(value ?? '').trim())
            throw new Error(t('请填写必填字段'));
          if (value == null || value === '') continue;
          body[key] =
            kind === 'lines'
              ? String(value)
                  .split(/\r?\n/)
                  .map((line) => line.trim())
                  .filter(Boolean)
              : key === 'seconds'
                ? String(value)
                : value;
        }
      }
      if (audioUpload && !file && !native)
        throw new Error(t('请选择要转写的音频文件'));
      let referenceURLs;
      const videoURLs = videoJSONUpload
        ? (values.image_url || '')
            .split(/\r?\n/)
            .map((row) => row.trim())
            .filter(Boolean)
        : [];
      if (videoJSONUpload && (referenceImages.length || videoURLs.length)) {
        if (!selectedVideoMode) throw new Error(t('请选择视频图片的用途'));
        if (referenceImages.length + videoURLs.length > videoLimit)
          throw new Error(
            t('当前模型最多支持 {{limit}} 张图片', { limit: videoLimit }),
          );
        if (videoURLs.some((url) => !/^https?:\/\//i.test(url)))
          throw new Error(t('请输入公开的参考图链接'));
      }
      if (
        !native &&
        videoTool &&
        !videoJSONUpload &&
        option?.video_image_url &&
        referenceImages.length === 0 &&
        values.image_url?.trim()
      ) {
        const url = values.image_url.trim();
        if (!/^https?:\/\//i.test(url))
          throw new Error(t('请输入公开的参考图链接'));
        body.image_url = url;
      }
      if (imageEdit) {
        if (!option?.image_edit) throw new Error(t('当前模型不支持图生图'));
        if (!native && imageUpload && !referenceImages.length)
          throw new Error(t('图生图需要参考图片'));
        if (!native && !imageUpload) {
          referenceURLs = (values.image || '')
            .split(/\r?\n/)
            .map((row) => row.trim())
            .filter(Boolean);
          if (
            !referenceURLs.length ||
            referenceURLs.some((url) => !/^https?:\/\//i.test(url))
          )
            throw new Error(t('请输入公开的参考图链接'));
          if (
            option.image_url_limit &&
            referenceURLs.length > option.image_url_limit
          )
            throw new Error(
              t('当前模型最多支持 {{limit}} 张图片', {
                limit: option.image_url_limit,
              }),
            );
        }
      }
      setSubmitting(true);
      setResponse(null);
      if (latestAudio.current) URL.revokeObjectURL(latestAudio.current);
      latestAudio.current = '';
      setAudio('');
      const docs = Array.isArray(body.documents)
        ? body.documents.map((item) =>
            typeof item === 'string' ? item : (item?.text ?? t('候选媒体')),
          )
        : [];
      if (rerankTool) docs.push(...inputFiles.map(({ file }) => file.name));
      setDocuments(docs);
      const uploads = fileField
        ? audioUpload
          ? file
            ? [file]
            : []
          : referenceImages.map((item) => item.file)
        : [];
      const uploadField =
        imageTool && uploads.length > 1 ? 'image[]' : fileField;
      const request = protocolRequest(
        endpoint,
        inputs.group,
        inputs.model,
        false,
        native,
        body,
      );
      if (imageTool) request.payload.image_edit = imageEdit;
      if (referenceURLs) request.payload.reference_urls = referenceURLs;
      const attachments = videoJSONUpload
        ? referenceImages
        : embeddingFiles.length
          ? embeddingFiles
          : inputFiles;
      const imageField = videoJSONUpload
        ? 'video_images'
        : embeddingFiles.length
          ? 'embedding_images'
          : 'input_images';
      if (videoJSONUpload && (attachments.length || videoURLs.length))
        request.payload.video_image_mode = selectedVideoMode;
      if (embeddingFiles.length && selectedEmbeddingMode)
        request.payload.embedding_mode = selectedEmbeddingMode;
      setDebugData((previous) => ({
        ...previous,
        request: {
          ...request.payload,
          ...(uploads.length && {
            [uploadField]: uploads.map((file) => file.name),
          }),
          ...((attachments.length || videoURLs.length) && {
            [imageField]: [
              ...attachments.map(({ file }) => file.name),
              ...videoURLs,
            ],
          }),
          ...(queryFile && { query_image: queryFile.name }),
        },
        response: null,
        timestamp: new Date().toISOString(),
        sseMessages: null,
        isStreaming: false,
      }));
      setActiveDebugTab(DEBUG_TABS.REQUEST);
      let url = request.url;
      const requestHeaders = headers();
      let payload;
      if (uploads.length) {
        const query = new URLSearchParams({
          group: inputs.group,
          model: inputs.model,
        });
        url += `?${query}`;
        payload = new FormData();
        for (const [key, value] of Object.entries({
          ...body,
          model: inputs.model,
        })) {
          payload.append(
            key,
            typeof value === 'object' ? JSON.stringify(value) : String(value),
          );
        }
        for (const upload of uploads) payload.append(uploadField, upload);
      } else {
        requestHeaders['Content-Type'] = 'application/json';
        const data = { ...request.payload };
        try {
          if (attachments.length || videoURLs.length) {
            const encoded = await Promise.all(
              attachments.map(({ file }) => readImageDataURL(file)),
            );
            data[imageField] = [...encoded, ...videoURLs];
          }
          if (queryFile) data.query_image = await readImageDataURL(queryFile);
        } catch {
          throw new Error(t('读取图片失败'));
        }
        payload = JSON.stringify(data);
      }
      if (embeddingFiles.length && chooseEmbeddingMode) setEmbeddingMode('');
      const res = await fetch(url, {
        method: 'POST',
        headers: requestHeaders,
        body: payload,
      });
      const data = await decode(res);
      const id = !res.ok
        ? data?.error?.task_id
        : res.headers.get('X-New-Api-Task-Id') ||
          (['openai-video', 'batch-generation'].includes(endpoint)
            ? data?.task_id || data?.data?.task_id || data?.id
            : null) ||
          (endpoint === 'image-generation' ? data?.task_id : null) ||
          (endpoint === 'vertex-native' && data?.name?.startsWith('operations/')
            ? data.name.slice('operations/'.length)
            : null);
      if (typeof id === 'string' && id) {
        localStorage.setItem(taskKey, id);
        if (mounted.current) setTaskId(id);
      }
      if (!mounted.current) return;
      setDebugData((previous) => ({
        ...previous,
        response:
          data instanceof Blob
            ? `${data.type} · ${data.size} bytes`
            : JSON.stringify(data, null, 2),
      }));
      setActiveDebugTab(DEBUG_TABS.RESPONSE);
      checkResponse(res, data);
      if (data instanceof Blob) {
        latestAudio.current = URL.createObjectURL(data);
        setAudio(latestAudio.current);
      } else {
        setResponse(data);
      }
    } catch (err) {
      if (mounted.current) setError(err.message);
    } finally {
      if (mounted.current) setSubmitting(false);
    }
  };

  const queryTask = async () => {
    if (!taskId || submitting || querying) return;
    setQuerying(true);
    setError('');
    try {
      const res = await fetch(
        `${API_ENDPOINTS.TASKS}/${encodeURIComponent(taskId)}`,
        { headers: headers() },
      );
      const data = await decode(res);
      checkResponse(res, data);
      if (mounted.current) {
        setResponse(data);
        setDebugData((prev) => ({
          ...prev,
          response: JSON.stringify(data, null, 2),
          sseMessages: null,
          isStreaming: false,
        }));
        setActiveDebugTab(DEBUG_TABS.RESPONSE);
      }
    } catch (err) {
      if (mounted.current) setError(err.message);
    } finally {
      if (mounted.current) setQuerying(false);
    }
  };

  const changeValue = (key, value) =>
    setValues((previous) => ({ ...previous, [key]: value }));
  return (
    <Card bordered={false} className='h-full overflow-auto'>
      <div className='mb-5 flex items-center justify-between gap-3'>
        <Typography.Title heading={5}>{label}</Typography.Title>
        <Button size='small' onClick={onToggleDebugPanel}>
          {showDebugPanel ? t('隐藏调试') : t('显示调试')}
        </Button>
      </div>
      <form
        onSubmit={submit}
        onPaste={
          imageTool ||
          inputUpload ||
          videoTool ||
          (!native && (embeddingTool || rerankTool || moderationTool))
            ? pasteReference
            : undefined
        }
        className='space-y-4'
      >
        {imageTool && (
          <div className='space-y-1'>
            <RadioGroup
              type='button'
              aria-label={t('图像生成模式')}
              value={imageEdit ? 'edit' : 'generate'}
              onChange={(event) => {
                setImageEdit(event.target.value === 'edit');
                setError('');
              }}
            >
              <Radio value='generate'>{t('文生图')}</Radio>
              <Radio value='edit' disabled={!option?.image_edit}>
                {t('图生图')}
              </Radio>
            </RadioGroup>
            <Typography.Text type='tertiary' className='block text-xs'>
              {imageEdit ? '/v1/images/edits' : '/v1/images/generations'}
              {ready &&
                !option?.image_edit &&
                ` · ${t('当前模型不支持图生图')}`}
            </Typography.Text>
          </div>
        )}
        {fields && (
          <label className='flex items-center gap-2 text-sm'>
            <Switch size='small' checked={native} onChange={setNative} />
            {t('自定义请求体模式')}
          </label>
        )}
        {native ? (
          <label className='block space-y-1 text-sm'>
            <span>{t('请求体 JSON')}</span>
            <TextArea
              value={draftText}
              onChange={onDraftChange}
              rows={10}
              className='font-mono'
            />
          </label>
        ) : (
          fields
            .filter((field) => field[2] !== 'file')
            .map(([name, title, kind, required]) => (
              <label key={name} className='block space-y-1 text-sm'>
                <span>
                  {t(title)}
                  {required && !textOptional(name) && ' *'}
                </span>
                {kind === 'textarea' || kind === 'lines' ? (
                  <TextArea
                    value={values[name] || ''}
                    onChange={(value) => changeValue(name, value)}
                    disabled={name === 'query' && !!queryFile}
                    rows={3}
                  />
                ) : kind === 'number' ? (
                  <InputNumber
                    value={values[name]}
                    onChange={(value) => changeValue(name, value)}
                    min={1}
                    precision={0}
                  />
                ) : Array.isArray(kind) ? (
                  <Select
                    aria-label={t(title)}
                    value={values[name]}
                    optionList={kind.map((value) => ({ label: value, value }))}
                    onChange={(value) => changeValue(name, value)}
                    showClear
                    style={{ width: '100%' }}
                  />
                ) : (
                  <Input
                    value={
                      values[name] ??
                      (name === 'voice' ? option?.default_voice : '') ??
                      ''
                    }
                    onChange={(value) => changeValue(name, value)}
                  />
                )}
              </label>
            ))
        )}
        {videoJSONUpload && (
          <div className='space-y-1'>
            <RadioGroup
              type='button'
              aria-label={t('视频图片用途')}
              value={selectedVideoMode}
              onChange={(event) => {
                setVideoMode(event.target.value);
                setError('');
              }}
            >
              {videoModes.includes('frames') && (
                <Radio value='frames'>{t('首帧／尾帧')}</Radio>
              )}
              {videoModes.includes('reference') && (
                <Radio value='reference'>{t('参考图片')}</Radio>
              )}
            </RadioGroup>
            {selectedVideoMode === 'frames' && (
              <Typography.Text className='block text-xs' type='tertiary'>
                {t('按顺序将第一张用作首帧，第二张用作尾帧')}
              </Typography.Text>
            )}
          </div>
        )}
        {showReference && (
          <PlaygroundImagePicker
            label={referenceLabel}
            files={referenceImages}
            limit={imageLimit}
            onAdd={addImages}
            onRemove={(id) =>
              setImages((previous) => previous.filter((item) => item.id !== id))
            }
          />
        )}
        {inputUpload && option?.image_query && (
          <PlaygroundImagePicker
            label={t('查询图片')}
            files={queryImages}
            limit={1}
            target='query'
            onAdd={(files) => addImages(files, 'query')}
            onRemove={(id) =>
              setQueryImages((previous) =>
                previous.filter((item) => item.id !== id),
              )
            }
          />
        )}
        {embeddingUpload &&
          (chooseEmbeddingMode || embeddingFiles.length > 0) && (
            <RadioGroup
              type='button'
              aria-label={t('向量生成方式')}
              value={selectedEmbeddingMode}
              disabled={submitting || embeddingFiles.length === 0}
              onChange={(event) => {
                setEmbeddingMode(event.target.value);
                setError('');
              }}
            >
              {embeddingModes.includes('separate') && (
                <Radio value='separate'>{t('逐项生成向量')}</Radio>
              )}
              {embeddingModes.includes('joint') && (
                <Radio value='joint'>{t('整组生成一个向量')}</Radio>
              )}
            </RadioGroup>
          )}
        {imageEdit && option?.image_edit && !imageUpload && !native && (
          <label className='block space-y-1 text-sm'>
            <span>{t('参考图链接（每行一个）')} *</span>
            <TextArea
              rows={3}
              placeholder='https://'
              value={values.image || ''}
              onChange={(value) => changeValue('image', value)}
            />
            <Typography.Text type='tertiary' className='block text-xs'>
              {t('请输入公开的参考图链接')}
            </Typography.Text>
          </label>
        )}
        {videoJSONUpload && option?.video_image_url && (
          <label className='block space-y-1 text-sm'>
            <span>{t('参考图链接（每行一个）')}</span>
            <TextArea
              rows={3}
              value={values.image_url || ''}
              onChange={(value) => changeValue('image_url', value)}
            />
            {referenceImages.length > 0 && (
              <Typography.Text className='block text-xs' type='tertiary'>
                {t('链接图片排在上传图片之后')}
              </Typography.Text>
            )}
          </label>
        )}
        {videoTool &&
          !videoJSONUpload &&
          option?.video_image_url &&
          !native &&
          !referenceImages.length && (
            <label className='block space-y-1 text-sm'>
              <span>{t('参考图链接')}</span>
              <Input
                placeholder='https://'
                value={values.image_url || ''}
                onChange={(value) => changeValue('image_url', value)}
              />
            </label>
          )}
        {!imageTool && !videoTool && fileField && (
          <label className='block space-y-1 text-sm'>
            <span className='block'>
              {t(audioUpload ? '音频文件' : '参考图片')}
            </span>
            <input
              type='file'
              accept={audioUpload ? 'audio/*,video/mp4' : 'image/*'}
              onChange={(event) => setFile(event.target.files?.[0] ?? null)}
            />
          </label>
        )}
        <Button
          htmlType='submit'
          theme='solid'
          type='primary'
          loading={submitting}
          disabled={!ready || querying}
        >
          {t('发送请求')}
        </Button>
      </form>
      {taskId && (
        <div className='mt-5 flex flex-wrap items-center gap-3'>
          <Typography.Text copyable className='break-all'>
            {taskId}
          </Typography.Text>
          <Button
            size='small'
            loading={querying}
            disabled={submitting}
            onClick={queryTask}
          >
            {t('查询结果')}
          </Button>
        </div>
      )}
      {error && (
        <div role='alert' className='mt-3'>
          <Typography.Text type='danger'>{error}</Typography.Text>
        </div>
      )}
      {(response != null || audio) && (
        <section className='mt-5 space-y-3 border-t border-[var(--semi-color-border)] pt-4'>
          <Typography.Title heading={6}>{t('结果')}</Typography.Title>
          {audio && <MediaResult url={audio} type='audio' />}
          <ToolResult
            data={response}
            endpoint={endpoint}
            family={option?.family}
            documents={documents}
          />
        </section>
      )}
    </Card>
  );
}
