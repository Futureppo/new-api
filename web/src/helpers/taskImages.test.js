import { describe, expect, test } from 'bun:test';
import { extractImageResults } from './taskImages';

describe('task image results', () => {
  test('HY images preserve dimensions and exclude audio and duplicates', () => {
    const images = extractImageResults({
      data: {
        outcome: {
          media_urls: [
            {
              type: 'image',
              url: 'https://example.com/a.png',
              width: 1920,
              height: 1080,
            },
            { type: 'image', url: 'https://example.com/a.png' },
            { type: 'image', url: 'https://example.com/b.png' },
            { type: 'audio', url: 'https://example.com/a.mp3' },
          ],
        },
      },
    });
    expect(images).toHaveLength(2);
    expect(images[0].width).toBe(1920);
    expect(images[1].src).toEndWith('/b.png');
  });
  test('supports existing OpenAI URL and base64 results', () => {
    expect(
      extractImageResults({
        data: { data: [{ url: 'https://example.com/a.png' }] },
      })[0].src,
    ).toEndWith('/a.png');
    expect(extractImageResults({ data: [{ b64_json: 'abc' }] })[0].src).toBe(
      'data:image/png;base64,abc',
    );
  });
  test('uses result URL or thumbnail as fallback and handles malformed media', () => {
    expect(
      extractImageResults({ result_url: 'https://example.com/result.png' }),
    ).toHaveLength(1);
    expect(
      extractImageResults({
        data: {
          outcome: {
            thumbnail_image_url: 'https://example.com/thumbnail.png',
            media_urls: {},
          },
        },
      })[0].src,
    ).toEndWith('/thumbnail.png');
    expect(extractImageResults(null)).toEqual([]);
  });
});
