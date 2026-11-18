// @ts-check
"use strict";


import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const ROOT_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");


function splitComment(line) {
  const idx = line.indexOf("//");
  if (idx === -1) {
    return { code: line, comment: "" };
  }
  return { code: line.slice(0, idx), comment: line.slice(idx + 2) };
}

function tokenize(code) {
  return code
    .trim()
    .split(/\s+/)
    .filter((t) => t.length > 0)
    .map((t) => t.replace(/^["`]|["`]$/g, ""));
}

function parseGoWork(goWorkPath) {
  const content = fs.readFileSync(goWorkPath, "utf-8");
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

function parseGoMod(goModPath) {
  const content = fs.readFileSync(goModPath, "utf-8");
  let modulePath = null;
  let currentBlock = null;
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
      const directive = (blockOpen)[1];
      currentBlock = directive === "module" ? null : directive;
      continue;
    }

    const inline = trimmed.match(/^(\w+)\s+(.+)$/);
    if (!inline) continue;
    const [, directive, rest] = (inline);
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

function collectModules(rootDir) {
  const goWorkPath = path.join(rootDir, "go.work");
  if (!fs.existsSync(goWorkPath)) {
    throw new Error(`go.work not found at ${goWorkPath}`);
  }
  const dirs = parseGoWork(goWorkPath);

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

function printReport(modules) {
  const workspacePaths = new Set(modules.map((m) => m.modulePath));
  const externalUsage = new Map();
  const workspaceUsage = new Map();

  console.log("=== go.work direct dependency report ===");
  console.log(`modules: ${modules.length}\n`);

  for (const mod of modules) {
    const external = [];
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
