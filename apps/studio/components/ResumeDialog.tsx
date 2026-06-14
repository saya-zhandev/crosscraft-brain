'use client';
import { useState } from 'react';
import { PlayCircle } from 'lucide-react';
import { api } from '@/lib/client';
import { toast } from '@/components/ui/sonner';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog';

/**
 * In-UI resume for a waiting (durable-wait) execution. The posted JSON becomes the
 * resumed node's output item. Used from the editor run panel and the Runs page.
 */
export function ResumeDialog({
  executionId,
  onResumed,
  size = 'sm',
}: {
  executionId: string;
  onResumed?: () => void;
  size?: 'sm' | 'default';
}) {
  const [open, setOpen] = useState(false);
  const [body, setBody] = useState('{\n  \n}');
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    let payload: unknown = {};
    try {
      payload = JSON.parse(body || '{}');
    } catch {
      toast.error('Resume payload must be valid JSON.');
      return;
    }
    setBusy(true);
    try {
      await api.resume(executionId, payload);
      toast.success('Run resumed.');
      setOpen(false);
      onResumed?.();
    } catch (e) {
      toast.error('Resume failed', { description: (e as Error).message });
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size={size} variant="secondary">
          <PlayCircle />
          Resume
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Resume waiting run</DialogTitle>
          <DialogDescription>
            This run is paused at a “Wait” node. The JSON below becomes the resume payload (the
            node’s next output item).
          </DialogDescription>
        </DialogHeader>
        <Textarea mono rows={8} value={body} onChange={(e) => setBody(e.target.value)} />
        <DialogFooter>
          <Button variant="ghost" onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy}>
            Resume run
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
