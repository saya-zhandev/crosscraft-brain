'use client';
import * as React from 'react';
import { cn } from '@/lib/utils';

export interface TextareaProps extends React.TextareaHTMLAttributes<HTMLTextAreaElement> {
  /** Render with a monospace font (for code / JSON / expressions). */
  mono?: boolean;
}

export const Textarea = React.forwardRef<HTMLTextAreaElement, TextareaProps>(
  ({ className, mono, ...props }, ref) => (
    <textarea
      ref={ref}
      className={cn(
        'flex min-h-[72px] w-full rounded-md border border-input bg-bg px-3 py-2 text-sm text-text shadow-sm transition-colors',
        'placeholder:text-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:border-accent',
        'disabled:cursor-not-allowed disabled:opacity-50',
        mono && 'font-mono text-[12.5px] leading-relaxed',
        className,
      )}
      {...props}
    />
  ),
);
Textarea.displayName = 'Textarea';
