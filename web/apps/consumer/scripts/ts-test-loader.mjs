/**
 * Resolves extensionless relative imports to `.ts` for Node's native type stripping.
 */
import { existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const APP_ROOT = join(dirname(fileURLToPath(import.meta.url)), "..");

export function resolve(specifier, context, nextResolve) {
  if (specifier.startsWith("@/")) {
    const withoutAlias = specifier.slice(2);
    const tsCandidate = join(APP_ROOT, `${withoutAlias}.ts`);
    const tsxCandidate = join(APP_ROOT, `${withoutAlias}.tsx`);
    if (existsSync(tsCandidate)) {
      return { shortCircuit: true, url: pathToFileURL(tsCandidate).href };
    }
    if (existsSync(tsxCandidate)) {
      return { shortCircuit: true, url: pathToFileURL(tsxCandidate).href };
    }
  }
  if (specifier.startsWith(".") && !specifier.endsWith(".ts") && !specifier.endsWith(".js") && !specifier.endsWith(".mjs")) {
    const parent = context.parentURL ? fileURLToPath(context.parentURL) : process.cwd();
    const candidate = join(dirname(parent), `${specifier}.ts`);
    if (existsSync(candidate)) {
      return { shortCircuit: true, url: pathToFileURL(candidate).href };
    }
  }
  return nextResolve(specifier, context);
}
