// MonacoEditor — Monaco code editor loaded from CDN (jsDelivr) at runtime.
//
// Why CDN instead of npm install?
//   The monaco-editor package is ~3 MB unminified. Adding it as a Vite
//   dependency complicates the bundle (web workers, AMD loader, CSS, etc.).
//   Loading from a CDN keeps the build simple and lets us upgrade Monaco
//   without rebuilding the app.
//
//   Loader approach:
//     1. Inject a <script> tag for `monaco-loader` from jsDelivr. This
//        small script (~1 KB) sets up the AMD `require` that Monaco needs.
//     2. Once loaded, call `require(['vs/editor/editor.main'], ...)` which
//        fetches the main editor bundle from the same CDN.
//     3. Register custom languages (Rego) before creating an editor.
//
//   Fallback:
//     If the CDN script never resolves (offline, blocked, network error),
//     a timer fires and we render a plain <textarea> with the same value /
//     onChange contract. The parent component doesn't need to know.
//
//   Error boundary:
//     A small class component catches render-time exceptions (e.g. Monaco
//     throwing inside a worker callback) and also falls back to <textarea>.

import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
  type CSSProperties,
} from 'react';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------


export type MonacoLanguage =
  | 'bash'
  | 'powershell'
  | 'python'
  | 'javascript'
  | 'json'
  | 'yaml'
  | 'rego'
  | 'plaintext';

export type MonacoTheme = 'vs-dark' | 'light';

export interface MonacoEditorProps {
  value: string;
  onChange: (next: string) => void;
  language?: MonacoLanguage;
  height?: number | string;
  theme?: MonacoTheme;
  readOnly?: boolean;
  options?: Record<string, unknown>;
  className?: string;
  /** Minimum rows to show in the fallback textarea (used until height is known). */
  minRows?: number;
  placeholder?: string;
  /** Accessible label for the editor (applied to the fallback textarea). */
  ariaLabel?: string;
  /** ID of element that describes the editor (applied to the fallback textarea). */
  ariaDescribedBy?: string;
}

export interface MonacoEditorHandle {
  /** Force-layout the editor (useful after parent container resizes). */
  layout: () => void;
}

// ---------------------------------------------------------------------------
// CDN constants
// ---------------------------------------------------------------------------

const CDN_BASE = 'https://cdn.jsdelivr.net/npm/monaco-editor@0.52.2/min';
const LOADER_URL = `${CDN_BASE}/vs/loader.js`;
/** How long to wait for the CDN script before giving up and using fallback. */
const CDN_TIMEOUT_MS = 8000;

/**
 * Minimal type definitions for the parts of the Monaco API we touch.
 * We avoid pulling in `monaco-editor` types (which assume an npm install).
 */
export interface MonacoEditor {
  getValue(): string;
  setValue(val: string): void;
  onDidChangeModelContent(cb: () => void): void;
  updateOptions(opts: Record<string, unknown>): void;
  setModel(monaco: unknown, model: unknown): void;
  getModel(): unknown;
  dispose(): void;
  layout(): void;
  focus(): void;
}

export interface MonacoTextModel {
  setValue(val: string): void;
  getValue(): string;
  dispose(): void;
}

export interface MonacoNamespace {
  editor: {
    create(
      dom: HTMLElement,
      opts: Record<string, unknown>,
    ): MonacoEditor;
    defineTheme(name: string, theme: Record<string, unknown>): void;
    setTheme(name: string): void;
  };
  languages: {
    register({ id }: { id: string }): void;
    setMonarchTokensProvider(id: string, provider: unknown): void;
    registerCompletionItemProvider(id: string, provider: unknown): unknown;
  };
  KeyMod: Record<string, number>;
  KeyCode: Record<string, number>;
}

// ---------------------------------------------------------------------------
// Module-level state: singleton loader + pending callbacks
// ---------------------------------------------------------------------------

let monacoPromise: Promise<MonacoNamespace> | null = null;
let loaderScript: HTMLScriptElement | null = null;

export function injectLoaderScript(): Promise<void> {
  return new Promise((resolve, reject) => {
    if (loaderScript && document.querySelector(`script[src="${LOADER_URL}"]`)) {
      // Already injected; wait for it to call require.config.
      if ((window as unknown as { require?: { config?: unknown } }).require?.config) {
        resolve();
        return;
      }
      // Script tag exists but hasn't finished loading. Poll briefly.
      const start = Date.now();
      const id = setInterval(() => {
        if ((window as unknown as { require?: { config?: unknown } }).require?.config) {
          clearInterval(id);
          resolve();
        } else if (Date.now() - start > CDN_TIMEOUT_MS) {
          clearInterval(id);
          reject(new Error('Monaco loader script timed out'));
        }
      }, 100);
      return;
    }
    const s = document.createElement('script');
    s.src = LOADER_URL;
    s.async = true;
    s.onload = () => resolve();
    s.onerror = () => reject(new Error(`Failed to load Monaco loader from ${LOADER_URL}`));
    document.head.appendChild(s);
    loaderScript = s;
  });
}

export function loadMonaco(): Promise<MonacoNamespace> {
  if (monacoPromise) return monacoPromise;

  monacoPromise = new Promise<MonacoNamespace>((resolve, reject) => {
    const timeoutId = window.setTimeout(() => {
      reject(new Error(`Monaco CDN load timed out after ${CDN_TIMEOUT_MS}ms`));
    }, CDN_TIMEOUT_MS);

    injectLoaderScript()
      .then(() => {
        const w = window as unknown as {
          require: {
            config(opts: Record<string, unknown>): void;
            (
              deps: string[],
              cb: (...args: unknown[]) => void,
              errback?: (err: unknown) => void,
            ): void;
          };
        };

        // Point AMD loader at the same CDN for Monaco's own files.
        w.require.config({ paths: { vs: CDN_BASE } });

        // Monaco loads a few internal dependencies. We list them in the
        // array so require() fetches them in parallel from the CDN.
        w.require(
          ['vs/editor/editor.main'],
          (_editorExports: unknown, monacoExports: unknown) => {
            window.clearTimeout(timeoutId);
            const monaco = monacoExports as MonacoNamespace;
            registerCustomLanguages(monaco);
            resolve(monaco);
          },
          (err: unknown) => {
            window.clearTimeout(timeoutId);
            reject(err instanceof Error ? err : new Error(String(err)));
          },
        );
      })
      .catch((err: Error) => {
        window.clearTimeout(timeoutId);
        reject(err);
      });
  });

  // If we fail, reset so a subsequent mount can retry.
  monacoPromise.catch(() => {
    monacoPromise = null;
  });

  return monacoPromise;
}

// ---------------------------------------------------------------------------
// Custom language registration: Rego
// ---------------------------------------------------------------------------

const REGO_KEYWORDS = new Set([
  'package', 'import', 'as', 'default', 'else', 'false', 'true',
  'not', 'some', 'in', 'every', 'if', 'then', 'with', 'contains',
]);

export function registerCustomLanguages(monaco: MonacoNamespace): void {
  // Monaco ships with many languages (bash, python, javascript, json, yaml,
  // powershell via a built-in token provider, plaintext) but not Rego.
  // Register a minimal Rego tokenizer so the editor gets basic
  // highlighting: comments, strings, numbers, and keywords.
  if (monaco.languages.register) {
    monaco.languages.register({ id: 'rego' });
    monaco.languages.setMonarchTokensProvider('rego', {
      keywords: Array.from(REGO_KEYWORDS),
      tokenizer: {
        root: [
          [/#.*$/, 'comment'],
          [/"(?:[^"\\]|\\.)*"/, 'string'],
          [/`(?:[^`\\]|\\.)*`/, 'string'],
          [/\b\d+(\.\d+)?\b/, 'number'],
          [
            /[a-zA-Z_][a-zA-Z0-9_]*/,
            { cases: { '@keywords': 'keyword', '@default': 'identifier' } },
          ],
        ],
      },
    } as unknown as Parameters<typeof monaco.languages.setMonarchTokensProvider>[1]);
  }
}

