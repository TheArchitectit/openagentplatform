// cn — dependency-light className combiner.
//
// Joins truthy className inputs (strings, arrays, objects, falsy values)
// into a single space-separated string. Built on `clsx`, which is added
// as a direct dependency in package.json. We intentionally do NOT pull
// in `tailwind-merge` here; this codebase doesn't depend on it and
// adding it would mean a new dep without an immediate caller.
//
// Typical use:
//
//   import { cn } from '@/lib/cn';
//   <div className={cn('p-4 rounded', isActive && 'bg-blue-500', className)} />

import { clsx, type ClassValue } from "clsx";

/** Combine className fragments into a single string. */
export function cn(...inputs: ClassValue[]): string {
	return clsx(...inputs);
}

export default cn;
