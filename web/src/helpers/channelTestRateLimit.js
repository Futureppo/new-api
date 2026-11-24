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

export const formatChannelTestRateLimit = (rateLimit, t) => {
  const info = rateLimit || {};
  const hasValue = (value) =>
    value !== undefined && value !== null && String(value).trim() !== '';
  const valueOrMissing = (value) =>
    hasValue(value) ? String(value) : t('上游未返回');
  const tokens = hasValue(info.limit_tokens)
    ? info.limit_tokens
    : info.limit_project_tokens;
  const limits = [
    `RPM: ${valueOrMissing(info.limit_requests)}`,
    `TPM: ${valueOrMissing(tokens)}`,
  ];
  const appendLimit = (value, unit) => {
    if (hasValue(value)) limits.push(`${unit}: ${value}`);
  };
  appendLimit(info.limit_input_tokens, 'ITPM');
  appendLimit(info.limit_output_tokens, 'OTPM');
  appendLimit(
    info.limit_priority_input_tokens,
    t('优先级 ${unit}').replace('${unit}', 'ITPM'),
  );
  appendLimit(
    info.limit_priority_output_tokens,
    t('优先级 ${unit}').replace('${unit}', 'OTPM'),
  );

  // Project limits and remaining values must come from the same quota bucket.
  const remainingTokens = hasValue(info.limit_tokens)
    ? info.remaining_tokens
    : hasValue(info.limit_project_tokens)
      ? info.remaining_project_tokens
      : info.remaining_tokens;
  const hasRemaining =
    hasValue(info.remaining_requests) || hasValue(remainingTokens);
  return {
    limits: t('限速: ${limits}').replace('${limits}', limits.join(' / ')),
    remaining: hasRemaining
      ? t('剩余：{{requests}} 次请求 / {{tokens}} token', {
          requests: valueOrMissing(info.remaining_requests),
          tokens: valueOrMissing(remainingTokens),
        })
      : '',
  };
};
