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
  useCallback,
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
interface MonacoEditor {
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

interface MonacoTextModel {
  setValue(val: string): void;
  getValue(): string;
  dispose(): void;
}

interface MonacoNamespace {
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

type LoadResult = { monaco: MonacoNamespace } | { error: Error };

let monacoPromise: Promise<MonacoNamespace> | null = null;
let loaderScript: HTMLScriptElement | null = null;

function injectLoaderScript(): Promise<void> {
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

function loadMonaco(): Promise<MonacoNamespace> {
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
            (deps: string[], cb: (...args: unknown[]) => void): void;
          };
        };

        // Point AMD loader at the same CDN for Monaco's own files.
        w.require.config({ paths: { vs: CDN_BASE } });

        // Monaco loads a few internal dependencies. We list them in the
        // array so require() fetches them in parallel from the CDN.
        w.require(
          ['vs/editor/editor.main'],
          (_editorExports: unknown, monaco: MonacoNamespace) => {
            window.clearTimeout(timeoutId);
            registerCustomLanguages(monaco);
            resolve(monaco);
          },
          (err: Error) => {
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

function registerCustomLanguages(monaco: MonacoNamespace): void {
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

// ---------------------------------------------------------------------------
// Map our language names to Monaco's built-in language ids
// ---------------------------------------------------------------------------

const LANGUAGE_MAP: Record<MonacoLanguage, string> = {
  bash: 'shell',
  powershell: 'powershell',
  python: 'python',
  javascript: 'javascript',
  json: 'json',
  yaml: 'yaml',
  rego: 'rego',
  plaintext: 'plaintext',
};

export function resolveMonacoLanguage(lang: MonacoLanguage): string {
  return LANGUAGE_MAP[lang] ?? 'plaintext';
}

// ---------------------------------------------------------------------------
// Error boundary
// ---------------------------------------------------------------------------

interface ErrorBoundaryProps {
  fallback: React.ReactNode;
  children: React.ReactNode;
}

interface ErrorBoundaryState {
  hasError: boolean;
}

import { Component, type ReactNode } from 'react';

class SafeErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { hasError: false };
  }
  static getDerivedStateFromError(): ErrorBoundaryState {
    return { hasError: true };
  }
  override componentDidCatch(error: Error): void {
    // eslint-disable-next-line no-console
    console.warn('[MonacoEditor] render error, falling back to textarea:', error);
  }
  override render(): ReactNode {
    if (this.state.hasError) return this.props.fallback;
    return this.props.children;
  }
}

// ---------------------------------------------------------------------------
// Fallback textarea (used when Monaco can't load or throws)
// ---------------------------------------------------------------------------

interface FallbackProps {
  value: string;
  onChange: (next: string) => void;
  language: MonacoLanguage;
  height: number | string;
  readOnly?: boolean;
  minRows?: number;
  placeholder?: string;
  className?: string;
  ariaLabel?: string;
  ariaDescribedBy?: string;
}

function FallbackTextarea({
  value,
  onChange,
  language,
  height,
  readOnly,
  minRows = 12,
  placeholder,
  className,
  ariaLabel,
  ariaDescribedBy,
}: FallbackProps) {
  const heightStyle: CSSProperties =
    typeof height === 'number' ? { height: `${height}px` } : { height };
  return (
    <textarea
      value={value}
      onChange={(e) => onChange(e.target.value)}
      readOnly={readOnly}
      rows={minRows}
      spellCheck={false}
      data-language={language}
      placeholder={placeholder}
      aria-label={ariaLabel ?? `${language ?? 'code'} editor`}
      aria-describedby={ariaDescribedBy}
      className={
        'w-full bg-surface-primary text-text-primary p-3 resize-none outline-none text-sm font-mono leading-6 whitespace-pre overflow-auto ' +
        (className ?? '')
      }
      style={{ tabSize: 2, ...heightStyle }}
    />
  );
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export const MonacoEditor = forwardRef<MonacoEditorHandle, MonacoEditorProps>(
  function MonacoEditor(props, ref) {
    const {
      value,
      onChange,
      language = 'plaintext',
      height = 400,
      theme = 'vs-dark',
      readOnly = false,
      options,
      className,
      minRows = 12,
      placeholder,
      ariaLabel,
      ariaDescribedBy,
    } = props;

    const containerRef = useRef<HTMLDivElement>(null);
    const editorRef = useRef<MonacoEditor | null>(null);
    const modelRef = useRef<MonacoTextModel | null>(null);
    const monacoRef = useRef<MonacoNamespace | null>(null);
    // Keep the latest onChange in a ref so the change callback we register
    // with Monaco doesn't capture a stale closure.
    const onChangeRef = useRef(onChange);
    onChangeRef.current = onChange;

    const [status, setStatus] = useState<'loading' | 'ready' | 'fallback'>('loading');

    // ---- Imperative handle (layout) ----
    useImperativeHandle(
      ref,
      () => ({
        layout: () => {
          editorRef.current?.layout();
        },
      }),
      [],
    );

    // ---- Mount: load Monaco, create editor, model, and listeners ----
    useEffect(() => {
      let cancelled = false;
      const container = containerRef.current;
      if (!container) return;

      loadMonaco()
        .then((monaco) => {
          if (cancelled) return;
          monacoRef.current = monaco;

          // Register the vs-dark theme tuned for our slate palette.
          monaco.editor.defineTheme('oap-dark', {
            base: 'vs-dark',
            inherit: true,
            rules: [],
            colors: {
              'editor.background': '#020617', // slate-950
              'editor.foreground': '#e2e8f0', // slate-200
              'editorLineNumber.foreground': '#475569', // slate-600
              'editorLineNumber.activeForeground': '#94a3b8', // slate-400
              'editor.selectionBackground': '#334155', // slate-700
              'editor.lineHighlightBackground': '#0f172a', // slate-900
              'editorIndentGuide.background': '#1e293b', // slate-800
            },
          });

          const monacoLang = resolveMonacoLanguage(language);
          const editor = monaco.editor.create(container, {
            value,
            language: monacoLang,
            theme: theme === 'vs-dark' ? 'oap-dark' : 'light',
            readOnly,
            automaticLayout: true,
            minimap: { enabled: false },
            fontSize: 13,
            fontFamily:
              'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace',
            scrollBeyondLastLine: false,
            renderLineHighlight: 'line',
            padding: { top: 12, bottom: 12 },
            tabSize: 2,
            insertSpaces: true,
            wordWrap: 'on',
            ...options,
          });
          editorRef.current = editor;

          editor.onDidChangeModelContent(() => {
            const v = editor.getValue();
            onChangeRef.current(v);
          });

          setStatus('ready');
        })
        .catch((err: Error) => {
          if (cancelled) return;
          // eslint-disable-next-line no-console
          console.warn('[MonacoEditor] CDN load failed, using fallback:', err.message);
          setStatus('fallback');
        });

      return () => {
        cancelled = true;
        editorRef.current?.dispose();
        editorRef.current = null;
        modelRef.current?.dispose();
        modelRef.current = null;
        monacoRef.current = null;
      };
      // We intentionally exclude `value` and `language` from deps — we
      // push those changes into the existing editor below rather than
      // tearing it down on every keystroke.
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    // ---- Sync value changes into the editor (when not typing) ----
    useEffect(() => {
      const editor = editorRef.current;
      if (!editor) return;
      const current = editor.getValue();
      if (current !== value) {
        // Preserve cursor by setting on the model.
        editor.setValue(value);
      }
    }, [value]);

    // ---- Sync language ----
    useEffect(() => {
      const monaco = monacoRef.current;
      const editor = editorRef.current;
      if (!monaco || !editor) return;
      const model = editor.getModel() as MonacoTextModel | null;
      const monacoLang = resolveMonacoLanguage(language);
      if (model) {
        // Monaco's setModelLanguage is on the languages namespace, but we
        // keep the API surface narrow here. Use a safe dynamic call.
        const langApi = (monaco.editor as unknown as { setModelLanguage?: (m: unknown, l: string) => void });
        if (typeof langApi.setModelLanguage === 'function') {
          langApi.setModelLanguage(model, monacoLang);
        }
      }
    }, [language]);

    // ---- Sync readOnly / theme ----
    useEffect(() => {
      const monaco = monacoRef.current;
      const editor = editorRef.current;
      if (!editor) return;
      editor.updateOptions({ readOnly, ...(options ?? {}) });
      if (monaco) {
        monaco.editor.setTheme(theme === 'vs-dark' ? 'oap-dark' : theme);
      }
    }, [readOnly, theme, options]);

    // ---- Container size ----
    const containerStyle: CSSProperties = {
      height: typeof height === 'number' ? `${height}px` : height,
      minHeight: '120px',
    };

    // ---- Loading skeleton ----
    if (status === 'loading') {
      return (
        <div
          className={
            'rounded-md border border-border-subtle bg-surface-primary flex items-center justify-center ' +
            (className ?? '')
          }
          style={{ ...containerStyle, minHeight: '120px' }}
        >
          <div className="flex items-center gap-2 text-xs text-text-muted">
            <span
              className="inline-block h-3 w-3 rounded-full border-2 border-text-muted border-t-accent animate-spin"
              aria-hidden
            />
            Loading editor…
          </div>
        </div>
      );
    }

    // ---- Fallback (CDN failed) ----
    if (status === 'fallback') {
      return (
        <FallbackTextarea
          value={value}
          onChange={onChange}
          language={language}
          height={height}
          readOnly={readOnly}
          minRows={minRows}
          placeholder={placeholder}
          className={className}
          ariaLabel={ariaLabel}
          ariaDescribedBy={ariaDescribedBy}
        />
      );
    }

    // ---- Ready: render the Monaco container inside a SafeErrorBoundary ----
    return (
      <SafeErrorBoundary
        fallback={
          <FallbackTextarea
            value={value}
            onChange={onChange}
            language={language}
            height={height}
            readOnly={readOnly}
            minRows={minRows}
            placeholder={placeholder}
            className={className}
            ariaLabel={ariaLabel}
            ariaDescribedBy={ariaDescribedBy}
          />
        }
      >
        <div
          ref={containerRef}
          className={'rounded-md border border-border-subtle overflow-hidden ' + (className ?? '')}
          style={containerStyle}
          role="textbox"
          aria-multiline="true"
          aria-label={ariaLabel ?? `${language ?? 'code'} editor`}
          aria-describedby={ariaDescribedBy}
          aria-readonly={readOnly}
        />
      </SafeErrorBoundary>
    );
  },
);

export default MonacoEditor;
