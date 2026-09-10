import { describe, expect, test } from 'bun:test';
import type { CodexQuotaState } from '../src/types';
import {
  subscriptionExpiryMs,
  formatSubscriptionExpiry,
} from '../src/features/quota/subscriptionExpiry';

describe('account subscription expiry', () => {
  const expires = '2026-09-17T12:00:00Z';
  test('reads the existing Codex ID token subscription claim before quota is loaded', () => {
    expect(
      subscriptionExpiryMs(
        { name: 'account.json', id_token: { chatgpt_subscription_active_until: expires } },
        'codex'
      )
    ).toBe(Date.parse(expires));
  });
  test('prefers loaded subscription data and supports seconds or milliseconds', () => {
    const quota: CodexQuotaState = {
      status: 'success',
      windows: [],
      subscriptionActiveUntil: Date.parse(expires) / 1000,
    };
    expect(
      subscriptionExpiryMs(
        { name: 'account.json', subscription_active_until: '2026-09-10T12:00:00Z' },
        'codex',
        quota
      )
    ).toBe(Date.parse(expires));
    expect(
      subscriptionExpiryMs({ name: 'a', subscription_expires_at: Date.parse(expires) }, 'claude')
    ).toBe(Date.parse(expires));
  });
  test('supports the sub2api credential field, entitlement expiry, and nested subscription fields', () => {
    expect(
      subscriptionExpiryMs(
        { name: 'a', credentials: { subscription_expires_at: expires } },
        'codex'
      )
    ).toBe(Date.parse(expires));
    expect(
      subscriptionExpiryMs(
        { name: 'a', metadata: { subscription: { active_until: expires } } },
        'kimi'
      )
    ).toBe(Date.parse(expires));
  });
  test('does not mistake credential expiry or quota resets for subscription expiry', () => {
    expect(
      subscriptionExpiryMs(
        {
          name: 'a',
          expired: expires,
          expires_at: expires,
          exp: Date.parse(expires) / 1000,
          id_token: { exp: Date.parse(expires) / 1000 },
        },
        'codex'
      )
    ).toBeNull();
    expect(
      subscriptionExpiryMs({ name: 'a', subscription_active_until: 'invalid' }, 'codex')
    ).toBeNull();
    expect(subscriptionExpiryMs({ name: 'a', subscription_expires_at: 0 }, 'claude')).toBeNull();
  });
  test('formats the screenshot date and missing data', () => {
    expect(formatSubscriptionExpiry(new Date(2026, 8, 17, 12).getTime())).toBe('2026/9/17');
    expect(formatSubscriptionExpiry(null)).toBe('--');
  });
});
