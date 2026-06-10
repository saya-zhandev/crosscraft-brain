/**
 * Node registry — the spine. Node packages register their NodeDefinitions here;
 * the engine reads it to execute and the canvas reads (serializable) descriptors
 * to build the palette and config forms.
 */
import type { NodeDefinition, NodeGroup, ParamSchema, Port } from '@crosscraft/schema';

/** Serializable subset of a NodeDefinition — safe to send to the browser (no execute()). */
export interface NodeDescriptor {
  type: string;
  label: string;
  group: NodeGroup;
  icon?: string;
  description?: string;
  inputs: Port[];
  outputs: Port[];
  params: ParamSchema[];
  credentials?: string[];
  isTrigger?: boolean;
}

export class Registry {
  private defs = new Map<string, NodeDefinition>();

  register(...definitions: NodeDefinition[]): this {
    for (const def of definitions) {
      if (this.defs.has(def.type)) throw new Error(`Duplicate node type: ${def.type}`);
      this.defs.set(def.type, def);
    }
    return this;
  }

  get(type: string): NodeDefinition {
    const def = this.defs.get(type);
    if (!def) throw new Error(`Unknown node type: ${type}`);
    return def;
  }

  has(type: string): boolean {
    return this.defs.has(type);
  }

  all(): NodeDefinition[] {
    return [...this.defs.values()];
  }

  descriptors(): NodeDescriptor[] {
    return this.all().map((d) => ({
      type: d.type,
      label: d.label,
      group: d.group,
      icon: d.icon,
      description: d.description,
      inputs: d.inputs,
      outputs: d.outputs,
      params: d.params,
      credentials: d.credentials,
      isTrigger: d.isTrigger,
    }));
  }
}
