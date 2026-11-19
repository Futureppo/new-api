import { expect, test } from 'bun:test';
import { clipboardImages } from './playgroundImages';

test('collects every clipboard image in order', () => {
  const first = new File(['first'], 'first.png', { type: 'image/png' });
  const second = new File(['second'], 'second.jpg', { type: 'image/jpeg' });
  const text = new File(['text'], 'notes.txt', { type: 'text/plain' });

  expect(clipboardImages({ files: [first, text, second] })).toEqual([
    first,
    second,
  ]);
  const items = [first, text, second].map((file) => ({
    kind: 'file',
    type: file.type,
    getAsFile: () => file,
  }));
  expect(clipboardImages({ items })).toEqual([first, second]);
});
