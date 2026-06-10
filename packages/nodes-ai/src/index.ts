/** AI action nodes — LLM steps usable inside any workflow. */
import type { Item, Json, NodeDefinition } from '@crosscraft/schema';
import { complete, structured, MODELS } from './llm';

const itemsOrEmpty = (input: Item[]): Item[] => (input.length ? input : [{ json: {} }]);

// --- AI Summarize ------------------------------------------------------------
const summarize: NodeDefinition = {
  type: 'ai.summarize',
  label: 'AI Summarize',
  group: 'ai',
  icon: 'Sparkles',
  description: 'Summarize text with an LLM.',
  inputs: [{ id: 'main' }],
  outputs: [{ id: 'main' }],
  params: [
    { name: 'text', label: 'Text', type: 'expression', required: true, placeholder: '{{ $json.body }}' },
    { name: 'maxWords', label: 'Max words', type: 'number', default: 60 },
  ],
  async execute(ctx) {
    const out: Item[] = [];
    for (const _ of itemsOrEmpty(ctx.input)) {
      const text = String(ctx.params.text ?? '');
      const maxWords = Number(ctx.params.maxWords ?? 60);
      const summary = await complete({
        model: MODELS.fast,
        system: `Summarize the user's text in at most ${maxWords} words. Output only the summary.`,
        prompt: text,
      });
      out.push({ json: { summary } });
    }
    return { outputs: { main: out } };
  },
};

// --- AI Classify -------------------------------------------------------------
const classify: NodeDefinition = {
  type: 'ai.classify',
  label: 'AI Classify',
  group: 'ai',
  icon: 'Tags',
  description: 'Classify text into one of the provided categories.',
  inputs: [{ id: 'main' }],
  outputs: [{ id: 'main' }],
  params: [
    { name: 'text', label: 'Text', type: 'expression', required: true },
    { name: 'categories', label: 'Categories (JSON array)', type: 'json', default: ['urgent', 'normal'] },
  ],
  async execute(ctx) {
    const cats = Array.isArray(ctx.params.categories) ? (ctx.params.categories as string[]) : ['a', 'b'];
    const out: Item[] = [];
    for (const _ of itemsOrEmpty(ctx.input)) {
      const result = await structured<{ category: string; confidence: number }>({
        model: MODELS.fast,
        system: 'Classify the text into exactly one category.',
        prompt: `Categories: ${cats.join(', ')}\n\nText:\n${String(ctx.params.text ?? '')}`,
        toolName: 'classify',
        schema: {
          type: 'object',
          properties: {
            category: { type: 'string', enum: cats },
            confidence: { type: 'number' },
          },
          required: ['category'],
        },
      });
      out.push({ json: { category: result.category, confidence: result.confidence ?? null } });
    }
    return { outputs: { main: out } };
  },
};

// --- AI Extract --------------------------------------------------------------
const extract: NodeDefinition = {
  type: 'ai.extract',
  label: 'AI Extract',
  group: 'ai',
  icon: 'ScanText',
  description: 'Extract structured fields from text into a JSON object.',
  inputs: [{ id: 'main' }],
  outputs: [{ id: 'main' }],
  params: [
    { name: 'text', label: 'Text', type: 'expression', required: true },
    {
      name: 'fields',
      label: 'Fields to extract (JSON: { field: "description" })',
      type: 'json',
      default: { name: 'person name', amount: 'dollar amount' },
    },
  ],
  async execute(ctx) {
    const fields = (ctx.params.fields ?? {}) as Record<string, string>;
    const properties: Record<string, unknown> = {};
    for (const [k, desc] of Object.entries(fields)) properties[k] = { type: 'string', description: desc };
    const out: Item[] = [];
    for (const _ of itemsOrEmpty(ctx.input)) {
      const result = await structured<Record<string, Json>>({
        model: MODELS.fast,
        system: 'Extract the requested fields from the text. Use empty string if not present.',
        prompt: String(ctx.params.text ?? ''),
        toolName: 'extract',
        schema: { type: 'object', properties },
      });
      out.push({ json: result });
    }
    return { outputs: { main: out } };
  },
};

export const aiNodes: NodeDefinition[] = [summarize, classify, extract];
export { complete, structured, MODELS } from './llm';
