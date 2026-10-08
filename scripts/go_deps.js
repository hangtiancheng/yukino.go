// @ts-check
"use strict";

/**
 * Report direct dependencies of every Go module in the go.work workspace.
 *
 * Reads go.work to discover workspace modules, then parses each module's
 * go.mod and collects direct (non-indirect) require entries. Only dependency
 * names are reported; versions are ignored. Workspace-internal dependencies
 * are listed separately from external ones. Results are printed to stdout.
 *
 * CLI usage:
 *     node scripts/go_deps.js
 */

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

/** @type {string} */
const ROOT_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

/**
 * @typedef {object} WorkspaceModule
 * @property {string} dir          - Module directory relative to the repo root (posix separators).
 * @property {string} modulePath   - Module path declared in its go.mod.
 * @property {string[]} directDeps - Direct (non-indirect) dependency names, sorted and deduped.
 */

/**
 * Split a go.mod/go.work line into its code part and trailing comment.
 * @param {string} line
 * @returns {{ code: string, comment: string }}
 */
function splitComment(line) {
  const idx = line.indexOf("//");
  if (idx === -1) {
    return { code: line, comment: "" };
  }
  return { code: line.slice(0, idx), comment: line.slice(idx + 2) };
}

/**
 * Tokenize the code part of a go.mod line, stripping surrounding quotes.
 * @param {string} code
 * @returns {string[]}
 */
function tokenize(code) {
  return code
    .trim()
    .split(/\s+/)
    .filter((t) => t.length > 0)
    .map((t) => t.replace(/^["`]|["`]$/g, ""));
}

/**
 * Parse go.work and return the directories of the modules it uses.
 * Supports both `use ( ... )` blocks and single-line `use ./dir` directives.
 * @param {string} goWorkPath - Absolute path to the go.work file.
 * @returns {string[]} Module directories relative to the go.work location (posix separators).
 */
function parseGoWork(goWorkPath) {
  const content = fs.readFileSync(goWorkPath, "utf-8");
  /** @type {string[]} */
  const dirs = [];
  let inUseBlock = false;

  for (const rawLine of content.split("\n")) {
    const { code } = splitComment(rawLine);
    const trimmed = code.trim();
    if (trimmed.length === 0) continue;

    if (inUseBlock) {
      if (trimmed === ")") {
        inUseBlock = false;
        continue;
      }
      const tokens = tokenize(code);
      if (tokens.length > 0) {
        dirs.push(tokens[0].replace(/\\/g, "/"));
      }
      continue;
    }

    if (/^use\s*\($/.test(trimmed)) {
      inUseBlock = true;
      continue;
    }
    const single = trimmed.match(/^use\s+(.+)$/);
    if (single) {
      const tokens = tokenize(single[1]);
      if (tokens.length > 0) {
        dirs.push(tokens[0].replace(/\\/g, "/"));
      }
    }
  }
  return dirs;
}

/**
 * Parse a go.mod file and return its module path plus direct dependency names.
 * A require entry counts as direct unless its line carries an `// indirect`
 * comment. Versions are discarded; only dependency names are kept.
 * @param {string} goModPath - Absolute path to the go.mod file.
 * @returns {{ modulePath: string, directDeps: string[] }}
 */
function parseGoMod(goModPath) {
  const content = fs.readFileSync(goModPath, "utf-8");
  /** @type {string | null} */
  let modulePath = null;
  /** @type {string | null} */
  let currentBlock = null;
  /** @type {Set<string>} */
  const directDeps = new Set();

  for (const rawLine of content.split("\n")) {
    const { code, comment } = splitComment(rawLine);
    const trimmed = code.trim();
    if (trimmed.length === 0) continue;

    if (currentBlock !== null) {
      if (trimmed === ")") {
        currentBlock = null;
        continue;
      }
      if (currentBlock === "require" && !comment.trim().startsWith("indirect")) {
        const tokens = tokenize(code);
        if (tokens.length > 0) {
          directDeps.add(tokens[0]);
        }
      }
      continue;
    }

    const blockOpen = trimmed.match(/^(module|go|require|replace|exclude|retract|toolchain|tool)\s*\($/);
    if (blockOpen) {
      const directive = /** @type {RegExpMatchArray} */ (blockOpen)[1];
      currentBlock = directive === "module" ? null : directive;
      continue;
    }

    const inline = trimmed.match(/^(\w+)\s+(.+)$/);
    if (!inline) continue;
    const [, directive, rest] = /** @type {[string, string, string]} */ (inline);
    const tokens = tokenize(rest);
    if (directive === "module" && tokens.length > 0) {
      modulePath = tokens[0];
    } else if (
      directive === "require" &&
      tokens.length > 0 &&
      !comment.trim().startsWith("indirect")
    ) {
      directDeps.add(tokens[0]);
    }
  }

  return {
    modulePath: modulePath ?? "",
    directDeps: [...directDeps].sort(),
  };
}

/**
 * Scan the workspace declared by go.work and collect per-module reports.
 * @param {string} rootDir - Absolute path to the repo root containing go.work.
 * @returns {WorkspaceModule[]}
 */
function collectModules(rootDir) {
  const goWorkPath = path.join(rootDir, "go.work");
  if (!fs.existsSync(goWorkPath)) {
    throw new Error(`go.work not found at ${goWorkPath}`);
  }
  const dirs = parseGoWork(goWorkPath);

  /** @type {WorkspaceModule[]} */
  const modules = [];
  for (const dir of dirs) {
    const goModPath = path.join(rootDir, dir, "go.mod");
    if (!fs.existsSync(goModPath)) {
      console.warn(`warn: skipping ${dir} (no go.mod)`);
      continue;
    }
    const { modulePath, directDeps } = parseGoMod(goModPath);
    modules.push({
      dir: dir.replace(/^\.\//, ""),
      modulePath,
      directDeps,
    });
  }
  return modules;
}

/**
 * Print the per-module direct dependency report and a usage summary.
 * @param {WorkspaceModule[]} modules
 */
function printReport(modules) {
  const workspacePaths = new Set(modules.map((m) => m.modulePath));
  /** @type {Map<string, number>} */
  const externalUsage = new Map();
  /** @type {Map<string, number>} */
  const workspaceUsage = new Map();

  console.log("=== go.work direct dependency report ===");
  console.log(`modules: ${modules.length}\n`);

  for (const mod of modules) {
    /** @type {string[]} */
    const external = [];
    /** @type {string[]} */
    const internal = [];
    for (const dep of mod.directDeps) {
      if (workspacePaths.has(dep)) {
        internal.push(dep);
        workspaceUsage.set(dep, (workspaceUsage.get(dep) ?? 0) + 1);
      } else {
        external.push(dep);
        externalUsage.set(dep, (externalUsage.get(dep) ?? 0) + 1);
      }
    }

    console.log(`[${mod.dir}] ${mod.modulePath}`);
    console.log(
      `  direct deps: ${external.length} external, ${internal.length} workspace`,
    );
    for (const dep of external) {
      console.log(`    - ${dep}`);
    }
    for (const dep of internal) {
      console.log(`    - ${dep} (workspace)`);
    }
    console.log("");
  }

  console.log("=== summary ===");
  const sortedExternal = [...externalUsage.entries()].sort((a, b) =>
    a[0].localeCompare(b[0]),
  );
  console.log(`unique external direct deps: ${sortedExternal.length}`);
  for (const [dep, count] of sortedExternal) {
    console.log(`  ${dep} (used by ${count} module${count === 1 ? "" : "s"})`);
  }
  const sortedWorkspace = [...workspaceUsage.entries()].sort((a, b) =>
    a[0].localeCompare(b[0]),
  );
  console.log(`unique workspace direct deps: ${sortedWorkspace.length}`);
  for (const [dep, count] of sortedWorkspace) {
    console.log(`  ${dep} (used by ${count} module${count === 1 ? "" : "s"})`);
  }
}

/**
 * Entry point: collect modules from go.work and print the report.
 * @param {string} [rootDir] - Repo root override, defaults to the parent of scripts/.
 */
function main(rootDir) {
  const root = rootDir ?? ROOT_DIR;
  const modules = collectModules(root);
  printReport(modules);
}

if (
  process.argv[1] &&
  fileURLToPath(import.meta.url) === path.resolve(process.argv[1])
) {
  main();
}

export { main, collectModules, parseGoMod, parseGoWork, splitComment, tokenize };
