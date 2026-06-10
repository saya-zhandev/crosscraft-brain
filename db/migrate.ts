/** Applies db/schema.sql (and any vertical schema passed as args) to DATABASE_URL. */
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join, resolve } from 'node:path';
import pg from 'pg';

const here = dirname(fileURLToPath(import.meta.url));

async function main() {
  const url = process.env.DATABASE_URL;
  if (!url) throw new Error('DATABASE_URL is not set (did you create .env?)');

  const files = [join(here, 'schema.sql'), ...process.argv.slice(2).map((p) => resolve(p))];
  const pool = new pg.Pool({ connectionString: url });
  try {
    for (const f of files) {
      const sql = readFileSync(f, 'utf8');
      await pool.query(sql);
      console.log('applied', f);
    }
    console.log('migration complete');
  } finally {
    await pool.end();
  }
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
