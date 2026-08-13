// Script editor — create a new script.
//
// Route definition for /scripts/new. The page component lives in
// NewScriptPage.tsx; runtime options in script-runtime-options.tsx.

import { createFileRoute } from '@tanstack/react-router';
import { NewScriptPage } from './NewScriptPage';
import { RUNTIME_OPTIONS, defaultTemplate, RUNTIME_TO_MONACO } from './script-runtime-options';

export const Route = createFileRoute('/scripts/new')({
  component: NewScriptPage,
});

// Re-export runtime options for callers importing from this path.
export { RUNTIME_OPTIONS, defaultTemplate, RUNTIME_TO_MONACO } from './script-runtime-options';
