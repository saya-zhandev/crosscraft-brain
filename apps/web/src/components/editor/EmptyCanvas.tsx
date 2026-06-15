import { MousePointerClick } from 'lucide-react';

/** Friendly empty state shown over the canvas when there are no nodes yet. */
export function EmptyCanvas() {
  return (
    <div className="pointer-events-none absolute inset-0 grid place-items-center">
      <div className="flex max-w-xs flex-col items-center gap-3 text-center">
        <div className="grid size-12 place-items-center rounded-2xl border border-dashed border-border-2 bg-panel-2 text-accent-2">
          <MousePointerClick className="size-6" />
        </div>
        <div>
          <p className="text-sm font-semibold text-text">Drag a trigger to start</p>
          <p className="mt-1 text-[13px] text-muted">
            Pick a node from the palette and drop it here — or click it to add. Connect nodes
            handle-to-handle to build your flow.
          </p>
        </div>
      </div>
    </div>
  );
}
