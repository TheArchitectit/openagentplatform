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


import { injectLoaderScript, loadMonaco, registerCustomLanguages, type MonacoNamespace } from './monaco-loader'

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
        'w-full bg-slate-900 text-white p-3 resize-none outline-none text-sm font-mono leading-6 whitespace-pre overflow-auto ' +
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
            'rounded-xl border border-slate-800 bg-slate-900 flex items-center justify-center ' +
            (className ?? '')
          }
          style={{ ...containerStyle, minHeight: '120px' }}
        >
          <div className="flex items-center gap-2 text-xs text-gray-300">
            <span
              className="inline-block h-3 w-3 rounded-full border-2 border-gray-300 border-t-blue-500 animate-spin"
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
          className={'rounded-xl border border-slate-800 overflow-hidden ' + (className ?? '')}
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
