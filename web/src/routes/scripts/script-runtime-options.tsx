// Script runtime options and helpers for the New Script page.
//
// Extracted from routes/scripts/new.tsx to keep that route file focused on
// the NewScriptPage UI. Re-exported from new.tsx so callers keep working.

import {
  Terminal,
  Code2,
  Braces,
} from 'lucide-react';
import type { ScriptRuntime } from '@/lib/useScripts';
import type { MonacoLanguage } from '@/components/monaco-loader';

export const RUNTIME_OPTIONS: { value: ScriptRuntime; label: string; icon: typeof Terminal; placeholder: string }[] = [
  {
    value: 'bash',
    label: 'Bash',
    icon: Terminal,
    placeholder: '#!/usr/bin/env bash\nset -euo pipefail\n\necho "Hello, world!"\n',
  },
  {
    value: 'powershell',
    label: 'PowerShell',
    icon: Terminal,
    placeholder: '# PowerShell script\n$ErrorActionPreference = "Stop"\n\nWrite-Host "Hello, world!"\n',
  },
  {
    value: 'python',
    label: 'Python',
    icon: Code2,
    placeholder: '#!/usr/bin/env python3\nimport sys\n\ndef main():\n    print("Hello, world!")\n\nif __name__ == "__main__":\n    main()\n',
  },
  {
    value: 'node',
    label: 'Node',
    icon: Braces,
    placeholder: '// Node.js script\nconst main = async () => {\n  console.log("Hello, world!");\n};\n\nmain().catch(console.error);\n',
  },
];

export function defaultTemplate(rt: ScriptRuntime): string {
  return RUNTIME_OPTIONS.find((o) => o.value === rt)?.placeholder ?? '';
}

/** Map a script runtime to the Monaco language id we want for syntax highlighting. */
export const RUNTIME_TO_MONACO: Record<ScriptRuntime, MonacoLanguage> = {
  bash: 'bash',
  powershell: 'powershell',
  python: 'python',
  node: 'javascript',
};
