// PolicyEditor — public entry point.
//
// This file is kept as a thin re-export shim so existing imports
// (`import { PolicyEditor } from '@/components/policy-editor'`) keep working.
// The actual component implementation lives in PolicyEditorForm.tsx and the
// form constants/helpers in policy-editor-helpers.tsx.

export { PolicyEditor } from './PolicyEditorForm';
export { default } from './PolicyEditorForm';
export type { PolicyTemplate } from './policy-editor-helpers';
export type { PolicyEditorProps } from './policy-editor-helpers';
