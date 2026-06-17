import { createRootRoute, Outlet } from '@tanstack/react-router';
import { Sidebar } from '@/components/sidebar';
import { Header } from '@/components/header';
import { SkipToContent } from '@/lib/a11y';
import { SidebarProvider } from '@/lib/sidebar';

export const Route = createRootRoute({
  component: RootLayout,
});

function RootLayout() {
  return (
    <SidebarProvider>
      <div className="min-h-screen flex bg-surface-primary text-text-primary">
        <SkipToContent targetId="main-content" />
        <Sidebar />
        {/* Content column.
         * On desktop (>= 768px) the sidebar is in normal flow and pushes
         * the content to the right.  On mobile the sidebar is a fixed
         * overlay so the content takes the full viewport width. */}
        <div className="flex-1 flex flex-col min-h-screen min-w-0">
          <Header />
          <main
            id="main-content"
            className="flex-1 p-4 md:p-6 overflow-auto"
            tabIndex={-1}
          >
            <Outlet />
          </main>
        </div>
      </div>
    </SidebarProvider>
  );
}