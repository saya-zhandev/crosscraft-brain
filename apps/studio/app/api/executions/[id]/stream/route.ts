import { loadExecution, getExecutionSteps } from '@crosscraft/engine';

// Live monitoring via SSE. Polls the run and pushes status + steps until it finishes.
export async function GET(_req: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const encoder = new TextEncoder();

  const stream = new ReadableStream({
    async start(controller) {
      const send = (data: unknown) =>
        controller.enqueue(encoder.encode(`data: ${JSON.stringify(data)}\n\n`));

      let done = false;
      for (let i = 0; i < 600 && !done; i++) {
        const exec = await loadExecution(id);
        if (!exec) {
          send({ error: 'not found' });
          break;
        }
        const steps = await getExecutionSteps(id);
        send({ status: exec.status, waitingNodeId: exec.waitingNodeId, steps });
        if (exec.status === 'success' || exec.status === 'error') done = true;
        else await new Promise((r) => setTimeout(r, 700));
      }
      controller.close();
    },
  });

  return new Response(stream, {
    headers: {
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-cache, no-transform',
      Connection: 'keep-alive',
    },
  });
}
