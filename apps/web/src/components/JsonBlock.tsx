import { cn } from '@/lib/utils';

/** Read-only pretty-printed JSON in a scrollable mono block. */
export function JsonBlock({
  value,
  className,
  tone = 'default',
}: {
  value: unknown;
  className?: string;
  tone?: 'default' | 'error';
}) {
  const text = typeof value === 'string' ? value : JSON.stringify(value, null, 2);
  return (
    <pre
      className={cn(
        'max-h-60 overflow-auto whitespace-pre-wrap break-words rounded-md border border-border bg-bg p-2.5 font-mono text-[11px] leading-relaxed',
        tone === 'error' ? 'text-err' : 'text-text',
        className,
      )}
    >
      {text}
    </pre>
  );
}
