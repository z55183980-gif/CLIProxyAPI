import { describe, expect, test } from 'bun:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { Select } from '../src/components/ui/Select';

const options = [
  { value: '', label: 'Choose a proxy' },
  { value: 'backup', label: 'Backup proxy' },
];
const render = (value: string, required = false, disabled = false) =>
  renderToStaticMarkup(
    createElement(Select, {
      id: 'backup-proxy',
      ariaLabel: 'Backup proxy',
      value,
      options,
      required,
      disabled,
      onChange: () => {},
    })
  );

describe('shared Select', () => {
  test('exposes a named collapsed combobox without submitting its parent form', () => {
    const markup = render('backup');
    expect(markup).toContain('role="combobox"');
    expect(markup).toContain('aria-label="Backup proxy"');
    expect(markup).toContain('aria-expanded="false"');
    expect(markup).toContain('type="button"');
    expect(markup).not.toContain('role="option"');
    expect(markup).not.toContain('<input');
  });

  test('keeps required form validation for empty and stale selections', () => {
    for (const value of ['', 'deleted-proxy']) {
      const markup = render(value, true);
      expect(markup).toContain('aria-required="true"');
      expect(markup).toMatch(/<input[^>]*required=""[^>]*value=""/);
    }
    expect(render('backup', true)).toMatch(/<input[^>]*required=""[^>]*value="backup"/);
  });

  test('disables the trigger and validation control together', () => {
    const markup = render('', true, true);
    expect(markup).toMatch(/<button[^>]*disabled=""/);
    expect(markup).toMatch(/<input[^>]*disabled=""[^>]*tabindex="-1"/);
  });
});
