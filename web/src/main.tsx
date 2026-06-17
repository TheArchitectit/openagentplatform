import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { Toaster } from 'sonner';
import { App } from './app';
import { SkipToContent } from './lib/a11y';
import './styles.css';

const container = document.getElementById('root');
if (!container) throw new Error('Root element #root not found');

createRoot(container).render(
  <StrictMode>
    {/*
     * SkipToContent is rendered at the very top so it is the first
     * focusable element in the DOM.  It becomes visible only when it
     * receives keyboard focus, and jumps to the #main-content element
     * in the root layout.
     */}
    <SkipToContent targetId="main-content" />
    <App />
    <Toaster
      position="top-right"
      richColors
      closeButton
      toastOptions={{
        classNames: {
          toast: 'bg-surface-secondary border-border-subtle text-text-primary',
        },
      }}
    />
  </StrictMode>
);
