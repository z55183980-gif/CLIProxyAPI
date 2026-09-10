import type { AccountUsageWindows } from '@/services/api/usageWindows';

// Cache is scoped to the current connection generation and bounded to visible/recent accounts.
export class ChannelUsageCache {
  private generation = -1;
  private entries = new Map<string, { data: AccountUsageWindows; at: number; revision: string }>();

  get(generation: number, account: string, revision: string, now: number) {
    this.selectGeneration(generation);
    const entry = this.entries.get(account);
    if (!entry || entry.revision !== revision || now - entry.at >= 60_000) return null;
    return entry.data;
  }

  set(
    generation: number,
    account: string,
    revision: string,
    data: AccountUsageWindows,
    now: number
  ) {
    this.selectGeneration(generation);
    this.entries.delete(account);
    this.entries.set(account, { data, at: now, revision });
    if (this.entries.size > 200) this.entries.delete(this.entries.keys().next().value!);
  }

  private selectGeneration(generation: number) {
    if (generation !== this.generation) {
      this.entries.clear();
      this.generation = generation;
    }
  }
}

export const channelUsageCache = new ChannelUsageCache();
