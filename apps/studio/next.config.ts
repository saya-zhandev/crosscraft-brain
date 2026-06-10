import type { NextConfig } from 'next';
import { config as loadEnv } from 'dotenv';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

// Load the monorepo-root .env so the engine (DATABASE_URL, secrets) works in dev without
// duplicating env files into the app.
const here = dirname(fileURLToPath(import.meta.url));
loadEnv({ path: resolve(here, '../../.env') });

const nextConfig: NextConfig = {
  // Consume workspace TS packages directly (no per-package build step).
  transpilePackages: ['@crosscraft/schema', '@crosscraft/engine', '@crosscraft/nodes-core'],
  serverExternalPackages: ['pg'],
};

export default nextConfig;
