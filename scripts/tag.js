// @ts-check
"use strict";


import fs from "node:fs";
import { spawnSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const ROOT_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

const MODULES = {
  cache: "libs/yukino_cache",
  http: "libs/yukino_http",
  orm: "libs/yukino_orm",
  rpc: "libs/yukino_rpc",
};

function run(command) {
  console.log(`> ${command.join(" ")}`);
  const result = spawnSync(command[0], command.slice(1), { stdio: "inherit" });
  if (result.status !== 0) {
    throw new Error(
      `Command failed: ${command.join(" ")} (exit ${result.status ?? "?"})`,
    );
  }
}

function normalizeVersion(ver) {
  return ver.startsWith("v") ? ver : `v${ver}`;
}

function isValidVersion(ver) {
  return /^v\d+\.\d+\.\d+/.test(ver);
}

function readPackageVersion() {
  const pkgPath = path.join(ROOT_DIR, "package.json");
  const pkg = JSON.parse(fs.readFileSync(pkgPath, "utf-8"));
  const ver = (pkg.version);
  if (!ver) {
    throw new Error("package.json does not contain a version field");
  }
  return normalizeVersion(ver);
}


function main(versions) {
  let resolved;

  if (!versions || Object.keys(versions).length === 0) {
    const ver = readPackageVersion();
    console.log(`No modules specified, using package.json version: ${ver}\n`);
    resolved = (
      Object.fromEntries(Object.keys(MODULES).map((k) => [k, ver]))
    );
  } else {
    resolved = versions;
  }

  const tagged = [];

  for (const [key, mod] of Object.entries(MODULES)) {
    const raw = resolved[ (key)];
    if (!raw) continue;
    const ver = normalizeVersion(raw);
    if (!isValidVersion(ver)) {
      throw new Error(
        `invalid version for ${key}: "${raw}" (expected [v]X.Y.Z)`,
      );
    }
    const tag = `${mod}/${ver}`;
    run(["git", "tag", tag]);
    run(["git", "push", "origin", tag]);
    tagged.push(tag);
  }

  if (tagged.length === 0) {
    console.log("No modules specified.");
  } else {
    console.log(`\nTagged and pushed: ${tagged.join(", ")}`);
  }
}

function parseArgs(argv) {
  const args = argv.slice(2);
  if (args.length === 0) {
    return {};
  }

  const versions = {};
  for (const arg of args) {
    const m = arg.match(/^--(\w+)=(.+)$/);
    if (!m) {
      console.error(`invalid argument: ${arg}`);
      process.exit(1);
    }
    const [, key, ver] = m;
    if (!(key in MODULES)) {
      console.error(
        `unknown module: ${key} (expected: ${Object.keys(MODULES).join(", ")})`,
      );
      process.exit(1);
    }
    versions[ (key)] = ver;
  }
  return versions;
}

if (
  process.argv[1] &&
  fileURLToPath(import.meta.url) === path.resolve(process.argv[1])
) {
  main(parseArgs(process.argv));
}

export {
  main,
  parseArgs,
  normalizeVersion,
  isValidVersion,
  readPackageVersion,
  MODULES,
};
