import { useEffect, useState } from 'react';
import { Plus, KeyRound, Trash2, ShieldCheck } from 'lucide-react';
import { api, type CredentialRow } from '@/lib/client';
import { AppHeader } from '@/components/AppHeader';
import { toast } from '@/components/ui/sonner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { Label } from '@/components/ui/label';
import { Badge } from '@/components/ui/badge';
import { Card } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog';

export default function Credentials() {
  const [rows, setRows] = useState<CredentialRow[] | null>(null);
  const [open, setOpen] = useState(false);

  const refresh = () => api.listCredentials().then(setRows).catch(() => setRows([]));
  useEffect(() => {
    refresh();
  }, []);

  const remove = async (c: CredentialRow) => {
    setRows((cur) => cur?.filter((x) => x.id !== c.id) ?? null);
    try {
      await api.deleteCredential(c.id);
      toast.success(`Deleted “${c.name}”`);
    } catch (e) {
      toast.error('Delete failed', { description: (e as Error).message });
      refresh();
    }
  };

  return (
    <div className="min-h-screen">
      <AppHeader />
      <main className="mx-auto max-w-6xl px-6 py-8">
        <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
          <div>
            <h1 className="text-2xl font-semibold">Credentials</h1>
            <p className="mt-1 flex items-center gap-1.5 text-sm text-muted">
              <ShieldCheck className="size-4 text-ok" />
              Encrypted at rest (AES-256-GCM). Secrets are never displayed after saving.
            </p>
          </div>
          <CreateDialog open={open} onOpenChange={setOpen} onCreated={refresh} />
        </div>

        {rows === null ? (
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-20" />
            ))}
          </div>
        ) : rows.length === 0 ? (
          <div className="grid place-items-center rounded-xl border border-dashed border-border bg-panel/40 py-20 text-center">
            <div className="flex max-w-sm flex-col items-center gap-3">
              <div className="grid size-12 place-items-center rounded-2xl bg-accent/15 text-accent-2">
                <KeyRound className="size-6" />
              </div>
              <div>
                <p className="text-sm font-semibold">No credentials yet</p>
                <p className="mt-1 text-[13px] text-muted">
                  Add API keys or connection secrets here, then reference them from node config.
                </p>
              </div>
              <Button onClick={() => setOpen(true)}>
                <Plus />
                Add credential
              </Button>
            </div>
          </div>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {rows.map((c) => (
              <Card key={c.id} className="flex items-center gap-3 p-4">
                <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-accent/15 text-accent-2">
                  <KeyRound className="size-4.5" />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[14px] font-semibold">{c.name}</div>
                  <Badge variant="secondary" className="mt-1 font-mono">
                    {c.type}
                  </Badge>
                </div>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="text-muted hover:text-err"
                  onClick={() => remove(c)}
                  aria-label={`Delete ${c.name}`}
                >
                  <Trash2 />
                </Button>
              </Card>
            ))}
          </div>
        )}
      </main>
    </div>
  );
}

function CreateDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onCreated: () => void;
}) {
  const [type, setType] = useState('');
  const [name, setName] = useState('');
  const [data, setData] = useState('{\n  "apiKey": ""\n}');
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    if (!type.trim() || !name.trim()) {
      toast.error('Type and name are required.');
      return;
    }
    let parsed: Record<string, unknown> = {};
    try {
      parsed = JSON.parse(data || '{}');
    } catch {
      toast.error('Credential data must be valid JSON.');
      return;
    }
    setBusy(true);
    try {
      await api.createCredential({ type: type.trim(), name: name.trim(), data: parsed });
      toast.success('Credential saved');
      onOpenChange(false);
      setType('');
      setName('');
      setData('{\n  "apiKey": ""\n}');
      onCreated();
    } catch (e) {
      toast.error('Save failed', { description: (e as Error).message });
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogTrigger asChild>
        <Button>
          <Plus />
          Add credential
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add credential</DialogTitle>
          <DialogDescription>The secret data is encrypted before it’s stored.</DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label>Type</Label>
              <Input value={type} onChange={(e) => setType(e.target.value)} placeholder="e.g. http, openai" />
            </div>
            <div className="space-y-1.5">
              <Label>Name</Label>
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Prod API key" />
            </div>
          </div>
          <div className="space-y-1.5">
            <Label>Data (JSON)</Label>
            <Textarea mono rows={6} value={data} onChange={(e) => setData(e.target.value)} />
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy}>
            Save credential
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
