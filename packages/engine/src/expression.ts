/**
 * Minimal {{ ... }} expression evaluator. Intentionally small — not full n8n parity.
 *
 * A param string may embed expressions: "Hello {{ $json.name }}". If the whole value is a
 * single expression ("{{ $json.qty }}"), the raw evaluated value is returned (type preserved);
 * otherwise expressions are stringified and interpolated.
 *
 * Scope exposed to expressions: $json, $input, $node('id'), $trigger, $now, JSON, Math.
 * Evaluated via Function in the server process — acceptable for self-hosted single-tenant MVP.
 */
import type { Item, Json } from '@crosscraft/schema';

export interface ExprScope {
  $json: Record<string, Json>;
  $input: Item[];
  $trigger: Item[];
  $node: (id: string) => Item[];
}

const EXPR_RE = /\{\{([\s\S]+?)\}\}/g;

function evalExpr(code: string, scope: ExprScope): unknown {
  const fn = new Function(
    '$json',
    '$input',
    '$trigger',
    '$node',
    '$now',
    'JSON',
    'Math',
    `"use strict"; return ( ${code} );`,
  );
  return fn(scope.$json, scope.$input, scope.$trigger, scope.$node, new Date(), JSON, Math);
}

/** Resolve one param value against the scope. Non-strings pass through untouched. */
export function resolveValue(value: unknown, scope: ExprScope): unknown {
  if (typeof value !== 'string' || !value.includes('{{')) return value;

  const whole = value.match(/^\s*\{\{([\s\S]+?)\}\}\s*$/);
  if (whole) {
    try {
      return evalExpr(whole[1]!, scope);
    } catch (e) {
      throw new Error(`Expression error in "${value}": ${(e as Error).message}`);
    }
  }

  return value.replace(EXPR_RE, (_m, code: string) => {
    let out: unknown;
    try {
      out = evalExpr(code, scope);
    } catch (e) {
      throw new Error(`Expression error in "${value}": ${(e as Error).message}`);
    }
    return out == null ? '' : typeof out === 'object' ? JSON.stringify(out) : String(out);
  });
}

/** Resolve every param of a node against the scope. */
export function resolveParams(
  params: Record<string, unknown>,
  scope: ExprScope,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(params)) out[k] = resolveValue(v, scope);
  return out;
}
