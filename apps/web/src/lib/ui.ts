import type { BadgeProps } from '@/components/ui/badge';

/**
 * Node-group + run-status color semantics. These are meaningful, not
 * decorative. Colors live in the token layer (globals.css); we reference the CSS vars by
 * name so a fork rebrands by overriding the var, never by editing a component.
 */

export type NodeGroupKey = 'trigger' | 'transform' | 'flow' | 'integration' | 'ai';

/** CSS var for a node group's accent — use in inline style or via color-mix. */
export const groupVar = (group: string): string => `var(--group-${group}, var(--accent))`;

/** Tailwind text-color class per group (utilities generated from the token layer). */
export const GROUP_TEXT: Record<string, string> = {
  trigger: 'text-group-trigger',
  transform: 'text-group-transform',
  flow: 'text-group-flow',
  integration: 'text-group-integration',
  ai: 'text-group-ai',
};

/** CSS var for a run/step status color. */
export const statusVar = (status?: string): string =>
  status && STATUS_VARS[status] ? STATUS_VARS[status] : 'var(--border)';

const STATUS_VARS: Record<string, string> = {
  running: 'var(--warn)',
  success: 'var(--ok)',
  error: 'var(--err)',
  waiting: 'var(--wait)',
};

/** Badge variant for an execution/step status. */
export const statusBadge = (status?: string): NonNullable<BadgeProps['variant']> => {
  switch (status) {
    case 'success':
      return 'success';
    case 'error':
      return 'error';
    case 'running':
      return 'warning';
    case 'waiting':
      return 'info';
    default:
      return 'secondary';
  }
};
