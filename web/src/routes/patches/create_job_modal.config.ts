// Wizard step definitions shared by CreateJobModal and its renderers.

export type WizardStep = 'patches' | 'targets' | 'configure' | 'review';

export const STEP_LABELS: Record<WizardStep, string> = {
  patches: 'Select Patches',
  targets: 'Select Targets',
  configure: 'Configure',
  review: 'Review & Submit',
};

export const STEPS: WizardStep[] = ['patches', 'targets', 'configure', 'review'];

// Loosely-typed catalog filter held locally by the create-job modal.
export interface CatalogFilterInput {
  severity?: string;
  category?: string;
  os?: string;
}
