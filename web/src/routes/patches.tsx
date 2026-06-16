import { createFileRoute } from '@tanstack/react-router';
import { Wrench, Construction } from 'lucide-react';

export const Route = createFileRoute('/patches')({
  component: PatchesStub,
});

function PatchesStub() {
  return (
    <div className="flex flex-col items-center justify-center text-center py-24">
      <div className="h-12 w-12 rounded-xl bg-slate-800 border border-slate-700 flex items-center justify-center mb-4">
        <Wrench className="h-6 w-6 text-slate-400" />
      </div>
      <h1 className="text-xl font-semibold text-slate-100">Patches</h1>
      <p className="text-slate-400 mt-2 max-w-sm flex items-center gap-2">
        <Construction className="h-4 w-4" />
        <span>Coming in Sprint 0.4</span>
      </p>
    </div>
  );
}
