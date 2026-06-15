'use client';
import { Trash2 } from 'lucide-react';
import type { NodeDescriptor } from '@crosscraft/schema';
import type { ParamSchema } from '@crosscraft/schema';
import { cn } from '@/lib/utils';
import { groupVar } from '@/lib/ui';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { CredentialPicker } from './CredentialPicker';

interface Props {
  descriptor: NodeDescriptor;
  name: string;
  params: Record<string, unknown>;
  onRename: (name: string) => void;
  onChange: (params: Record<string, unknown>) => void;
  onDelete: () => void;
}

function visible(p: ParamSchema, params: Record<string, unknown>): boolean {
  if (!p.showWhen) return true;
  return p.showWhen.equals.includes(params[p.showWhen.param]);
}

const asText = (v: unknown) => (v == null ? '' : typeof v === 'object' ? JSON.stringify(v, null, 2) : String(v));

export function Inspector({ descriptor, name, params, onRename, onChange, onDelete }: Props) {
  const set = (k: string, v: unknown) => onChange({ ...params, [k]: v });

  return (
    <div className="flex h-full flex-col">
      {/* header */}
      <div className="flex items-center justify-between gap-2 border-b border-border px-4 py-3">
        <div className="flex min-w-0 items-center gap-2">
          <span
            className="size-2 shrink-0 rounded-full"
            style={{ background: groupVar(descriptor.group) }}
          />
          <span className="truncate text-xs font-medium text-muted">{descriptor.label}</span>
        </div>
        <Button variant="ghost" size="icon-sm" onClick={onDelete} aria-label="Delete node" className="text-muted hover:text-err">
          <Trash2 />
        </Button>
      </div>

      <div className="flex-1 space-y-4 overflow-y-auto p-4">
        <Field label="Node name">
          <Input value={name} onChange={(e) => onRename(e.target.value)} />
        </Field>

        {descriptor.params.filter((p) => visible(p, params)).map((p) => {
          const val = params[p.name] ?? p.default ?? '';
          return (
            <Field key={p.name} label={p.label} required={p.required} hint={p.description}>
              {p.type === 'boolean' ? (
                <div className="pt-1">
                  <Switch checked={Boolean(val)} onCheckedChange={(c) => set(p.name, c)} />
                </div>
              ) : p.type === 'select' ? (
                <Select value={String(val)} onValueChange={(v) => set(p.name, v)}>
                  <SelectTrigger>
                    <SelectValue placeholder={p.placeholder} />
                  </SelectTrigger>
                  <SelectContent>
                    {(p.options ?? []).map((o) => (
                      <SelectItem key={o.value} value={o.value}>
                        {o.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : p.type === 'number' ? (
                <Input
                  type="number"
                  value={String(val)}
                  placeholder={p.placeholder}
                  onChange={(e) => set(p.name, e.target.value === '' ? '' : Number(e.target.value))}
                />
              ) : p.type === 'json' ? (
                <Textarea
                  mono
                  rows={5}
                  defaultValue={asText(val)}
                  placeholder={p.placeholder}
                  onBlur={(e) => {
                    try {
                      set(p.name, JSON.parse(e.target.value || '{}'));
                    } catch {
                      set(p.name, e.target.value);
                    }
                  }}
                />
              ) : p.type === 'expression' ? (
                <Textarea
                  mono
                  rows={3}
                  value={String(val)}
                  placeholder={p.placeholder}
                  onChange={(e) => set(p.name, e.target.value)}
                />
              ) : p.type === 'credential' ? (
                <CredentialPicker
                  credentialType={p.credentialType}
                  value={String(val)}
                  onChange={(id) => set(p.name, id)}
                />
              ) : (
                <Input value={String(val)} placeholder={p.placeholder} onChange={(e) => set(p.name, e.target.value)} />
              )}
            </Field>
          );
        })}
      </div>
    </div>
  );
}

function Field({
  label,
  required,
  hint,
  children,
}: {
  label: string;
  required?: boolean;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <Label className={cn(required && "after:ml-0.5 after:text-err after:content-['*']")}>{label}</Label>
      {children}
      {hint && <p className="text-[11px] leading-snug text-muted">{hint}</p>}
    </div>
  );
}
