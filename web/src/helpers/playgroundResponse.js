const tokenCount = (value) =>
  Number.isInteger(value) && value >= 0 ? value : undefined;

export function playgroundUsage(data) {
  const metadata = data.usageMetadata;
  if (metadata) {
    const input = tokenCount(metadata.promptTokenCount);
    const output = tokenCount(metadata.candidatesTokenCount);
    const thinking = tokenCount(metadata.thoughtsTokenCount);
    return {
      input:
        input === undefined
          ? undefined
          : input + (tokenCount(metadata.toolUsePromptTokenCount) ?? 0),
      output:
        output === undefined && thinking === undefined
          ? undefined
          : (output ?? 0) + (thinking ?? 0),
      cached: tokenCount(metadata.cachedContentTokenCount),
    };
  }
  const usage = data.response?.usage ?? data.usage ?? data.message?.usage;
  if (!usage || typeof usage !== 'object') return {};
  const anthropic =
    usage.usage_semantic === 'anthropic' ||
    'cache_read_input_tokens' in usage ||
    'cache_creation_input_tokens' in usage ||
    data.type === 'message_start' ||
    data.type === 'message_delta' ||
    data.type === 'message';
  const cacheAliases = [
    usage.prompt_tokens_details?.cached_tokens,
    usage.input_tokens_details?.cached_tokens,
    usage.prompt_cache_hit_tokens,
    usage.cache_read_input_tokens,
  ]
    .map(tokenCount)
    .filter((value) => value !== undefined);
  const cached = cacheAliases.length ? Math.max(...cacheAliases) : undefined;
  const cachePart = usage.cache_creation;
  const written = Math.max(
    tokenCount(
      usage.cache_creation_input_tokens ??
        usage.prompt_tokens_details?.cache_write_tokens ??
        usage.prompt_tokens_details?.cached_creation_tokens,
    ) ?? 0,
    (tokenCount(
      cachePart?.ephemeral_5m_input_tokens ??
        usage.claude_cache_creation_5m_tokens,
    ) ?? 0) +
      (tokenCount(
        cachePart?.ephemeral_1h_input_tokens ??
          usage.claude_cache_creation_1h_tokens,
      ) ?? 0),
  );
  return {
    input: tokenCount(usage.prompt_tokens ?? usage.input_tokens),
    output: tokenCount(usage.completion_tokens ?? usage.output_tokens),
    cached,
    ...(anthropic
      ? {
          anthropic: true,
          written:
            'cache_creation_input_tokens' in usage || cachePart || written > 0
              ? written
              : undefined,
        }
      : {}),
  };
}

export function replyStatistics(usage, started, completed, first, last) {
  const { output, cached } = usage;
  const input =
    usage.input === undefined
      ? undefined
      : usage.input +
        (usage.anthropic ? (cached ?? 0) + (usage.written ?? 0) : 0);
  const duration = first === undefined || last === undefined ? 0 : last - first;
  return {
    input,
    output,
    cached,
    cacheRate: input > 0 && cached !== undefined ? cached / input : undefined,
    firstMs: first === undefined ? undefined : first - started,
    durationMs: completed - started,
    tokensPerSecond:
      output !== undefined && duration > 0
        ? (output * 1000) / duration
        : undefined,
  };
}

const textOf = (value) => (typeof value === 'string' ? value : '');

export function playgroundReply(data, event = data.type) {
  const choice = data.choices?.[0];
  const message = choice?.delta ?? choice?.message;
  if (message)
    return {
      content: textOf(message.content),
      reasoning: textOf(message.reasoning_content || message.reasoning),
    };
  if (
    event === 'response.output_text.delta' ||
    event === 'response.refusal.delta'
  ) {
    return { content: textOf(data.delta) };
  }
  if (
    event === 'response.reasoning_summary_text.delta' ||
    event === 'response.reasoning_text.delta'
  ) {
    return { reasoning: textOf(data.delta) };
  }
  if (event === 'content_block_delta' || event === 'content_block_start') {
    const block = data.delta ?? data.content_block;
    return { content: textOf(block?.text), reasoning: textOf(block?.thinking) };
  }
  const parts = data.candidates?.[0]?.content?.parts;
  if (Array.isArray(parts)) {
    return {
      content: parts
        .filter((item) => !item.thought)
        .map((item) => textOf(item.text))
        .join(''),
      reasoning: parts
        .filter((item) => item.thought)
        .map((item) => textOf(item.text))
        .join(''),
    };
  }
  if (Array.isArray(data.content)) {
    return {
      content: data.content.map((item) => textOf(item.text)).join(''),
      reasoning: data.content.map((item) => textOf(item.thinking)).join(''),
    };
  }
  if (Array.isArray(data.output)) {
    return {
      content: data.output
        .flatMap((item) => item.content || [])
        .map((item) => textOf(item.text || item.refusal))
        .join(''),
      reasoning: data.output
        .filter((item) => item.type === 'reasoning')
        .flatMap((item) => item.summary || [])
        .map((item) => textOf(item.text))
        .join(''),
    };
  }
  return {};
}

export const playgroundStreamEvents = [
  'content_block_start',
  'content_block_delta',
  'content_block_stop',
  'message_start',
  'message_delta',
  'message_stop',
  'ping',
  'response.created',
  'response.in_progress',
  'response.output_item.added',
  'response.output_item.done',
  'response.content_part.added',
  'response.content_part.done',
  'response.output_text.delta',
  'response.output_text.done',
  'response.refusal.delta',
  'response.refusal.done',
  'response.reasoning_summary_part.added',
  'response.reasoning_summary_part.done',
  'response.reasoning_summary_text.delta',
  'response.reasoning_summary_text.done',
  'response.reasoning_text.delta',
  'response.reasoning_text.done',
  'response.function_call_arguments.delta',
  'response.function_call_arguments.done',
  'response.completed',
  'response.incomplete',
  'response.failed',
];
