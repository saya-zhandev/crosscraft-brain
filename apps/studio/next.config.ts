import type { NextConfig } from 'next';
import { config as loadEnv } from 'dotenv';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

// Load the monorepo-root .env so the engine (DATABASE_URL, secrets) works in dev without
// duplicating env files into the app.
const here = dirname(fileURLToPath(import.meta.url));
loadEnv({ path: resolve(here, '../../.env') });

const nextConfig: NextConfig = {
  // Self-contained server bundle for Docker (.next/standalone with traced deps).
  output: 'standalone',
  // The monorepo root is the workspace root for output-file tracing.
  outputFileTracingRoot: resolve(here, '../..'),
  // Consume workspace TS packages directly (no per-package build step).
  transpilePackages: [
    '@crosscraft/schema',
    '@crosscraft/engine',
    '@crosscraft/nodes-core',
    '@crosscraft/nodes-ai',
  ],
  serverExternalPackages: ['pg'],
  // Dev: proxy all /api/* calls to the Go backend (single source of truth for the
  // contract). beforeFiles runs before the filesystem, so it shadows the legacy
  // Next API routes. Override the target with GO_API_URL if needed.
  async rewrites() {
    const go = process.env.GO_API_URL ?? 'http://localhost:8080';
    return {
      beforeFiles: [{ source: '/api/:path*', destination: `${go}/api/:path*` }],
    };
  },
};

export default nextConfig;
