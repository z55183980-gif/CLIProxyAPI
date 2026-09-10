import { expect, test, spyOn } from 'bun:test';
import { authFilesApi } from '../src/services/api/authFiles';
import { apiClient } from '../src/services/api/client';

test('model tests preserve the credential, exact model, and cancellation signal', async () => {
  const result = {
    success: false,
    model: 'prefix/model-alias',
    latency_ms: 12,
    status_code: 401,
    error: 'Unauthorized',
  };
  const post = spyOn(apiClient, 'post').mockResolvedValue(result);
  const controller = new AbortController();
  try {
    expect(
      await authFilesApi.testModel('shared.json', 'index-b', result.model, controller.signal)
    ).toEqual(result);
    expect(post).toHaveBeenCalledWith(
      '/auth-files/test-model',
      {
        name: 'shared.json',
        auth_index: 'index-b',
        model: 'prefix/model-alias',
      },
      { signal: controller.signal, timeout: 0 }
    );
  } finally {
    post.mockRestore();
  }
});
