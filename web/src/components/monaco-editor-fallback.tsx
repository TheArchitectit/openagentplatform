// MonacoEditor fallbacks — error boundary and plain-textarea fallback.
//
// Extracted from monaco-editor.tsx to keep that file focused on the main
// MonacoEditor component. These are used when the CDN fails to load or when
// the editor throws at render time.

import { Component, type ReactNode } from 'react';
import type { CSSProperties } from 'react';
import type { MonacoLanguage } from './monaco-loader';

// ---------------------------------------------------------------------------
// Error boundary
// ---------------------------------------------------------------------------

interface ErrorBoundaryProps {
  fallback: ReactNode;
  children: ReactNode;
}

interface ErrorBoundaryState {
  hasError: boolean;
}

class SafeErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { hasError: false };
  }
  static getDerivedStateFromError(): ErrorBoundaryState {
    return { hasError: true };
  }
  override componentDidCatch(error: Error): void {
     
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

export { SafeErrorBoundary, FallbackTextarea };
export type { FallbackProps };
