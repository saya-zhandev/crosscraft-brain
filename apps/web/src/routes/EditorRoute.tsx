import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { api } from '@/lib/client';
import type { Workflow } from '@crosscraft/schema';
import Editor from '@/components/Editor';

// Client-side workflow load (replaces the Next server component that read the DB
// directly). Fetches via the same API the rest of the canvas uses.
export default function EditorRoute() {
  const { id = '' } = useParams();
  const [wf, setWf] = useState<Workflow | null>(null);
  const [notFound, setNotFound] = useState(false);

  useEffect(() => {
    if (!id) return;
    api
      .getWorkflow(id)
      .then(setWf)
      .catch(() => setNotFound(true));
  }, [id]);

  if (notFound) return <div className="p-10 text-sm text-muted">Workflow not found.</div>;
  if (!wf) return <div className="p-10 text-sm text-muted">Loading…</div>;
  return <Editor workflow={wf} />;
}
