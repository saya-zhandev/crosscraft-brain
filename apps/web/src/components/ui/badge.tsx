import * as React from 'react';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '@/lib/utils';

const badgeVariants = cva(
  'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-semibold transition-colors',
  {
    variants: {
      variant: {
        default: 'border-transparent bg-accent/15 text-accent-2',
        secondary: 'border-border bg-panel-2 text-muted',
        outline: 'border-border text-text',
        success: 'border-transparent bg-ok/15 text-ok',
        warning: 'border-transparent bg-warn/15 text-warn',
        error: 'border-transparent bg-err/15 text-err',
        info: 'border-transparent bg-wait/15 text-wait',
      },
    },
    defaultVariants: { variant: 'default' },
  },
);

export interface BadgeProps
  extends React.HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof badgeVariants> {}

export function Badge({ className, variant, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ variant }), className)} {...props} />;
}

export { badgeVariants };
