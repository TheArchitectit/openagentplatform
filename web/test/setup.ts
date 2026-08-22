// Vitest setup — runs once before the test suite.
//
// Node 26 ships an experimental `localStorage` global (gated behind
// --localstorage-file) that shadows jsdom's `localStorage` during Vitest's
// global population, leaving it `undefined`. jsdom's own `localStorage` is also
// unreliable under this collision, so we install a minimal in-memory shim on
// both globalThis and window for tests that touch `localStorage` directly
// (e.g. auth.test.ts). This is a toolchain shim, not a behavior change —
// auth.ts works normally in browsers.

class MemoryStorage implements Storage {
  private store = new Map<string, string>();
  get length(): number {
    return this.store.size;
  }
  clear(): void {
    this.store.clear();
  }
  getItem(key: string): string | null {
    return this.store.has(key) ? (this.store.get(key) as string) : null;
  }
  key(index: number): string | null {
    return Array.from(this.store.keys())[index] ?? null;
  }
  removeItem(key: string): void {
    this.store.delete(key);
  }
  setItem(key: string, value: string): void {
    this.store.set(key, String(value));
  }
  [name: number]: string;
}

const g = globalThis as unknown as {
  window?: Window & typeof globalThis;
  localStorage?: Storage;
};

const shim = new MemoryStorage();
if (!g.localStorage) {
  Object.defineProperty(g, 'localStorage', {
    value: shim,
    configurable: true,
    writable: true,
  });
}
if (g.window && !g.window.localStorage) {
  Object.defineProperty(g.window, 'localStorage', {
    value: shim,
    configurable: true,
    writable: true,
  });
}
