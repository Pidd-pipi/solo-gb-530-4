#!/usr/bin/env node
// Repeatable runner for the frontend store/page-state specs.
//
// Why this exists: the project has no browser test harness, but the store
// race logic is pure Angular/RxJS state that is fully testable under Node.
// esbuild bundles each src/**/*.spec.ts (with the real Angular classes and a
// controllable fake HttpClient) into one ESM file, then Node's built-in test
// runner executes it. No network, browser or zone.js is required.
//
// Run with: npm test
import { build } from 'esbuild';
import { readdirSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import process from 'node:process';
import { spawnSync } from 'node:child_process';

const root = process.cwd();

function findSpecs(dir) {
  const specs = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name === 'node_modules' || entry.name.startsWith('.')) continue;
      specs.push(...findSpecs(full));
    } else if (entry.name.endsWith('.spec.ts')) {
      specs.push(full);
    }
  }
  return specs.sort();
}

const specs = findSpecs(path.join(root, 'src'));
if (specs.length === 0) {
  console.error('No *.spec.ts files found under src/');
  process.exit(1);
}

// One virtual entry that imports every spec, so a single bundle runs all tests.
const entrySource = specs
  .map(spec => `import ${JSON.stringify(spec)};`)
  .join('\n');

const outfile = path.join(tmpdir(), `radiation-store-tests-${process.pid}.mjs`);

try {
  await build({
    stdin: { contents: entrySource, resolveDir: root, sourcefile: 'store-tests.ts', loader: 'ts' },
    bundle: true,
    platform: 'node',
    format: 'esm',
    target: 'node20',
    outfile,
    logLevel: 'warning',
  });
  // Execute the bundle under Node's test runner so the exit code reflects the
  // number of passing/failing cases.
  const result = spawnSync(process.execPath, ['--test', outfile], { stdio: 'inherit' });
  if (result.error) throw result.error;
  process.exitCode = result.status ?? 1;
} catch (error) {
  console.error(error);
  process.exitCode = 1;
} finally {
  try { rmSync(outfile, { force: true }); } catch { /* ignore cleanup */ }
}
