/** Credential file types. */

import type { RecentRequestBucket } from '@/utils/recentRequests';

export type AuthFileType =
  | 'qwen'
  | 'kimi'
  | 'gemini'
  | 'aistudio'
  | 'claude'
  | 'codex'
  | 'antigravity'
  | 'xai'
  | 'iflow'
  | 'vertex'
  | 'empty'
  | 'unknown';

export interface AuthFileItem {
  name: string;
  type?: AuthFileType | string;
  provider?: string;
  email?: string;
  projectId?: string;
  size?: number;
  authIndex?: string | number | null;
  runtimeOnly?: boolean | string;
  disabled?: boolean;
  unavailable?: boolean;
  status?: string;
  statusMessage?: string;
  lastRefresh?: string | number;
  modified?: number;
  priority?: number;
  concurrency?: number;
  currentConcurrency?: number;
  capacityEditable?: boolean;
  weight?: number;
  note?: string;
  success?: unknown;
  failed?: unknown;
  successCount?: number;
  failureCount?: number;
  recent_requests?: RecentRequestBucket[];
  recentRequests?: RecentRequestBucket[];
  [key: string]: unknown;
}

export interface AuthFilesResponse {
  files: AuthFileItem[];
  total?: number;
}
