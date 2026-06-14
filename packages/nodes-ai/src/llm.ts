/**
 * Provider abstraction for AI nodes + copilot. Anthropic Claude by default, but any
 * Anthropic-Messages-compatible endpoint works via env (e.g. DeepSeek's `/anthropic`):
 *   AI_BASE_URL   override base URL (else ANTHROPIC_BASE_URL, else Anthropic default)
 *   AI_API_KEY    override key       (else ANTHROPIC_API_KEY)
 *   AI_MODEL_FAST / AI_MODEL_SMART   override the model ids below
 */
import Anthropic from '@anthropic-ai/sdk';

// `||` (not `??`) so an empty env string — how Docker Compose passes an unset var — falls back.
export const MODELS = {
  fast: process.env.AI_MODEL_FAST || 'claude-haiku-4-5', // cheap in-node AI
  smart: process.env.AI_MODEL_SMART || 'claude-sonnet-4-6', // copilot / heavier reasoning
} as const;

let client: Anthropic | null = null;
function anthropic(): Anthropic {
  const apiKey = process.env.AI_API_KEY || process.env.ANTHROPIC_API_KEY;
  if (!apiKey) {
    throw new Error('No AI key set (AI_API_KEY or ANTHROPIC_API_KEY) — AI features are disabled.');
  }
  if (!client) {
    const baseURL = process.env.AI_BASE_URL || process.env.ANTHROPIC_BASE_URL;
    client = new Anthropic({ apiKey, ...(baseURL ? { baseURL } : {}) });
  }
  return client;
}

/** Plain text completion. */
export async function complete(opts: {
  system?: string;
  prompt: string;
  model?: string;
  maxTokens?: number;
}): Promise<string> {
  const res = await anthropic().messages.create({
    model: opts.model ?? MODELS.fast,
    max_tokens: opts.maxTokens ?? 1024,
    system: opts.system,
    messages: [{ role: 'user', content: opts.prompt }],
  });
  return res.content
    .filter((b): b is Anthropic.TextBlock => b.type === 'text')
    .map((b) => b.text)
    .join('');
}

/** Structured output via a single forced tool call; returns the tool input object. */
export async function structured<T = Record<string, unknown>>(opts: {
  system?: string;
  prompt: string;
  toolName: string;
  schema: Record<string, unknown>;
  model?: string;
  maxTokens?: number;
}): Promise<T> {
  const res = await anthropic().messages.create({
    model: opts.model ?? MODELS.smart,
    max_tokens: opts.maxTokens ?? 2048,
    system: opts.system,
    tools: [
      {
        name: opts.toolName,
        description: 'Return the result in this structure.',
        input_schema: opts.schema as Anthropic.Tool.InputSchema,
      },
    ],
    tool_choice: { type: 'tool', name: opts.toolName },
    messages: [{ role: 'user', content: opts.prompt }],
  });
  const tool = res.content.find((b): b is Anthropic.ToolUseBlock => b.type === 'tool_use');
  if (!tool) throw new Error('Model did not return a tool call');
  return tool.input as T;
}
