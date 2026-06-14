import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';

const SHORTCUTS: { keys: string; label: string }[] = [
  { keys: 'Ctrl + Z', label: 'Undo' },
  { keys: 'Ctrl + Shift + Z', label: 'Redo' },
  { keys: 'Ctrl + C', label: 'Copy selected node(s)' },
  { keys: 'Ctrl + V', label: 'Paste' },
  { keys: 'Ctrl + D', label: 'Duplicate selection' },
  { keys: 'Del / Backspace', label: 'Delete selection' },
  { keys: 'Ctrl + S', label: 'Save workflow' },
  { keys: 'Shift + drag', label: 'Box-select multiple nodes' },
  { keys: '?', label: 'Show this help' },
];

export function ShortcutsDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Keyboard shortcuts</DialogTitle>
          <DialogDescription>Speed up authoring on the canvas.</DialogDescription>
        </DialogHeader>
        <div className="divide-y divide-border">
          {SHORTCUTS.map((s) => (
            <div key={s.keys} className="flex items-center justify-between py-2">
              <span className="text-sm text-text">{s.label}</span>
              <kbd className="rounded border border-border bg-panel-2 px-2 py-0.5 font-mono text-[11px] text-muted">
                {s.keys}
              </kbd>
            </div>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  );
}
