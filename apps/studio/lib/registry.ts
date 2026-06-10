/** Server-only node registry, composed from the installed node packages.
 *  A fork edits ONLY this file to add its node packages. */
import { Registry } from '@crosscraft/engine';
import { coreNodes } from '@crosscraft/nodes-core';
import { aiNodes } from '@crosscraft/nodes-ai';
import { farmNodes } from '@crosscraft/nodes-farm'; // ← a fork adds its node pack here

let _registry: Registry | null = null;

export function registry(): Registry {
  if (!_registry) {
    _registry = new Registry().register(...coreNodes, ...aiNodes, ...farmNodes);
  }
  return _registry;
}
