'use client';
import { useEffect, useState } from 'react';
import Link from 'next/link';
import { api, type CredentialRow } from '@/lib/client';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';

/** Picks an existing credential of a given type. Stores the credential id in the param. */
export function CredentialPicker({
  credentialType,
  value,
  onChange,
}: {
  credentialType?: string;
  value: string;
  onChange: (id: string) => void;
}) {
  const [creds, setCreds] = useState<CredentialRow[]>([]);

  useEffect(() => {
    api.listCredentials().then(setCreds).catch(() => setCreds([]));
  }, []);

  const matches = credentialType ? creds.filter((c) => c.type === credentialType) : creds;

  if (matches.length === 0) {
    return (
      <p className="rounded-md border border-dashed border-border px-3 py-2 text-xs text-muted">
        No{credentialType ? ` ${credentialType}` : ''} credentials yet.{' '}
        <Link href="/credentials" className="text-accent-2 hover:underline">
          Add one →
        </Link>
      </p>
    );
  }

  return (
    <Select value={value || undefined} onValueChange={onChange}>
      <SelectTrigger>
        <SelectValue placeholder="Select a credential…" />
      </SelectTrigger>
      <SelectContent>
        {matches.map((c) => (
          <SelectItem key={c.id} value={c.id}>
            {c.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
