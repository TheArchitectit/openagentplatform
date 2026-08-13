// Script detail route — thin wrapper that declares the route and re-exports
// the page component. The page logic lives in ScriptDetailPage.tsx.

import { createFileRoute } from '@tanstack/react-router';
import { ScriptDetailPage } from './ScriptDetailPage';

export const Route = createFileRoute('/scripts/$scriptId')({
  component: ScriptDetailPage,
});

export { ScriptDetailPage };
