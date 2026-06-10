import { getWorkflow } from '@crosscraft/engine';
import Editor from '@/components/Editor';

export const dynamic = 'force-dynamic';

export default async function EditorPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const wf = await getWorkflow(id);
  if (!wf) {
    return <div style={{ padding: 40 }}>Workflow not found.</div>;
  }
  return <Editor workflow={wf} />;
}
