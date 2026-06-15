'use client';
import { Toaster as Sonner, type ToasterProps } from 'sonner';

/** Dark, token-styled toaster. Mounted once in the root layout. */
export function Toaster(props: ToasterProps) {
  return (
    <Sonner
      theme="dark"
      position="bottom-right"
      toastOptions={{
        classNames: {
          toast:
            'group toast !bg-panel-2 !border-border !text-text !rounded-lg !shadow-2xl',
          description: '!text-muted',
          actionButton: '!bg-accent !text-accent-fg',
          cancelButton: '!bg-panel-3 !text-muted',
          error: '!border-err/40',
          success: '!border-ok/40',
        },
      }}
      {...props}
    />
  );
}

export { toast } from 'sonner';
