// ErrorBoundary — top-level React error boundary that catches unhandled
// render errors in the component tree and renders a fallback UI instead of
// a blank page.
//
// Wire it in app.tsx around the RouterProvider so the route tree is guarded.
// This is NOT the Monaco-specific SafeErrorBoundary in monaco-editor.tsx
// (which is a local fallback for that one component).

import { Component, type ErrorInfo, type ReactNode } from "react";

interface ErrorBoundaryProps {
	children: ReactNode;
	/** Optional custom fallback; otherwise the default error panel is shown. */
	fallback?: ReactNode;
	/** Called when an error is caught. Useful for logging. */
	onError?: (error: Error, info: ErrorInfo) => void;
}

interface ErrorBoundaryState {
	error: Error | null;
}

export class ErrorBoundary extends Component<
	ErrorBoundaryProps,
	ErrorBoundaryState
> {
	constructor(props: ErrorBoundaryProps) {
		super(props);
		this.state = { error: null };
	}

	static getDerivedStateFromError(error: Error): ErrorBoundaryState {
		return { error };
	}

	override componentDidCatch(error: Error, info: ErrorInfo): void {
		 
		console.error("[ErrorBoundary] caught:", error, info);
		this.props.onError?.(error, info);
	}

	/** Reset the error so children re-render on next render cycle. */
	handleRetry = () => {
		this.setState({ error: null });
	};

	override render(): ReactNode {
		if (this.state.error) {
			if (this.props.fallback) return this.props.fallback;
			return (
				<DefaultFallback error={this.state.error} onRetry={this.handleRetry} />
			);
		}
		return this.props.children;
	}
}

// ---------------------------------------------------------------------------
// Default fallback rendered when no custom fallback is provided
// ---------------------------------------------------------------------------

function DefaultFallback({
	error,
	onRetry,
}: {
	error: Error;
	onRetry: () => void;
}) {
	return (
		<main
			className="flex min-h-dvh items-center justify-center bg-surface-primary p-6"
			role="alert"
			aria-live="assertive"
		>
			<div className="max-w-md w-full rounded-xl border border-border-subtle bg-surface-secondary p-8 shadow-lg text-center">
				<h1 className="text-xl font-semibold text-text-primary mb-3">
					Something went wrong
				</h1>
				<p className="text-sm text-text-secondary mb-6">
					An unexpected error occurred and the application cannot continue.
				</p>
				<pre className="mb-6 rounded-lg bg-surface-tertiary p-3 text-left text-xs text-text-secondary overflow-auto max-h-32">
					{error.message || "Unknown error"}
				</pre>
				<button
					type="button"
					className="inline-flex items-center rounded-lg bg-accent-primary px-4 py-2 text-sm font-medium text-white hover:bg-accent-secondary transition-colors"
					onClick={onRetry}
				>
					Try again
				</button>
			</div>
		</main>
	);
}

export default ErrorBoundary;
