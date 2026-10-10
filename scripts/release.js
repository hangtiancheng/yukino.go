#!/usr/bin/env node
// @ts-check

import { spawn } from "node:child_process";
import { lstat } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";


const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const NOTES =
  "Standalone executables for Linux, macOS, and Windows on x64 and arm64.";
const PROJECTS = [
  { name: "yukino-agent-proxy", directory: "services/agent_proxy" },
];
const PLATFORMS = [
  "linux-x64",
  "linux-arm64",
  "darwin-x64",
  "darwin-arm64",
  "win32-x64",
  "win32-arm64",
];

async function runCommand(command, args, options) {
  const completion = new Promise((resolve, reject) => {
    const child = spawn(command, [...args], {
      cwd: options.cwd,
      env: {
        ...process.env,
        GH_PROMPT_DISABLED: "1",
        GH_NO_UPDATE_NOTIFIER: "1",
      },
      stdio: options.inherit ? "inherit" : ["ignore", "pipe", "pipe"],
    });
    let stdout = "";
    let stderr = "";
    child.stdout?.setEncoding("utf8");
    child.stderr?.setEncoding("utf8");
    child.stdout?.on("data", ( chunk) => {
      stdout += chunk;
    });
    child.stderr?.on("data", ( chunk) => {
      stderr += chunk;
    });
    child.once("error", (error) =>
      reject(new Error(`Unable to run ${command}: ${error.message}`)),
    );
    child.once("close", (code, signal) => {
      const result = { code: code ?? 1, stdout, stderr };
      if (result.code !== 0 && !options.allowFailure) {
        const detail = stderr.trim() || stdout.trim();
        reject(
          new Error(
            `${command} ${args[0] ?? ""} ${signal ? `terminated by ${signal}` : `exited with code ${result.code}`}${detail ? `: ${detail}` : ""}`,
          ),
        );
      } else {
        resolve(result);
      }
    });
  });
  return completion;
}

function isObject(value) {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

async function readGitHub(runner, root, endpoint) {
  const result = await runner("gh", ["api", endpoint, "--include"], {
    cwd: root,
    allowFailure: true,
  });
  const status = Number(result.stdout.match(/^HTTP\/\S+\s+(\d{3})/m)?.[1]);
  if (status === 404) return null;
  if (
    result.code !== 0 ||
    status < 200 ||
    status >= 300 ||
    !Number.isFinite(status)
  ) {
    throw new Error(
      `GitHub lookup failed for ${endpoint}${Number.isFinite(status) ? ` (HTTP ${status})` : ""}: ${result.stderr.trim() || "invalid or unavailable response"}`,
    );
  }
  const separator = /\r?\n\r?\n/.exec(result.stdout);
  if (!separator)
    throw new Error(`GitHub returned invalid headers for ${endpoint}`);
  const value = JSON.parse(
    result.stdout.slice(separator.index + separator[0].length),
  );
  if (!isObject(value))
    throw new Error(`GitHub returned an invalid object for ${endpoint}`);
  return value;
}

export function parseArgs(args) {
  let help = false;
  let dryRun = false;
  const selected = new Set();
  for (const arg of args) {
    if (arg === "--help" || arg === "-h") {
      help = true;
      continue;
    }
    if (arg === "--dry-run") {
      dryRun = true;
      continue;
    }
    const project = PROJECTS.find((entry) => entry.name === arg);
    if (!project)
      throw new Error(
        `Unknown argument ${JSON.stringify(arg)}; expected a proxy name, --dry-run, or --help.`,
      );
    selected.add(project.name);
  }
  const names = PROJECTS.filter(
    (project) => selected.size === 0 || selected.has(project.name),
  ).map((project) => project.name);
  return { help, dryRun, names };
}

export async function release(names, options = {}) {
  const root = path.resolve(options.root ?? ROOT);
  const runner = options.runner ?? runCommand;
  const selected = PROJECTS.filter((project) => names.includes(project.name));
  if (
    selected.length === 0 ||
    names.some((name) => !PROJECTS.some((project) => project.name === name))
  ) {
    throw new Error("Select at least one supported proxy project.");
  }
  const [head, repository] = await Promise.all([
    runner("git", ["rev-parse", "HEAD"], { cwd: root }),
    runner(
      "gh",
      ["repo", "view", "--json", "nameWithOwner", "--jq", ".nameWithOwner"],
      { cwd: root },
    ),
  ]);
  const commit = head.stdout.trim();
  const repo = repository.stdout.trim();
  if (!/^[a-f0-9]{40}(?:[a-f0-9]{24})?$/i.test(commit))
    throw new Error("git returned an invalid HEAD commit.");
  if (!/^[a-zA-Z0-9_.-]+\/[a-zA-Z0-9_.-]+$/.test(repo))
    throw new Error("gh returned an invalid repository name.");
  const remoteCommit = await readGitHub(
    runner,
    root,
    `repos/${repo}/commits/${commit}`,
  );
  if (remoteCommit === null || remoteCommit.sha !== commit) {
    throw new Error(
      `HEAD is not available in ${repo}; push the commit before releasing.`,
    );
  }
  console.log(
    `Releasing ${selected.map((project) => project.name).join(", ")} to ${repo} at ${commit}`,
  );
  const states = [];
  for (const project of selected) {
    const current = await readGitHub(
      runner,
      root,
      `repos/${repo}/releases/tags/${project.name}`,
    );
    if (current !== null && current.tag_name !== project.name)
      throw new Error("GitHub returned a mismatched release tag.");
    if (current?.immutable === true)
      throw new Error(
        `Release ${project.name} is immutable and cannot be overwritten.`,
      );
    const tag = await readGitHub(
      runner,
      root,
      `repos/${repo}/git/ref/tags/${project.name}`,
    );
    if (tag !== null && tag.ref !== `refs/tags/${project.name}`)
      throw new Error("GitHub returned a mismatched tag reference.");
    states.push({
      project,
      releaseExists: current !== null,
      tagExists: tag !== null,
      assets: PLATFORMS.map((platform) =>
        path.join(
          root,
          project.directory,
          "bin",
          `${project.name}-${platform}`,
        ),
      ),
    });
  }
  for (const state of states) {
    console.log(`Building ${state.project.name}`);
    await runner(
      process.execPath,
      [path.join(root, state.project.directory, "build.mjs")],
      { cwd: root, inherit: true },
    );
    for (const asset of state.assets) {
      const file = await lstat(asset);
      if (!file.isFile() || file.size === 0)
        throw new Error(`Missing or invalid release executable: ${asset}`);
    }
  }
  const afterBuild = await runner("git", ["rev-parse", "HEAD"], { cwd: root });
  if (afterBuild.stdout.trim() !== commit)
    throw new Error("HEAD changed during the build; start the release again.");

  async function publish(args) {
    if (options.dryRun) {
      console.log(`[dry-run] gh ${JSON.stringify(args)}`);
    } else {
      await runner("gh", args, { cwd: root, inherit: true });
    }
  }
  for (const state of states) {
    const tag = state.project.name;
    const metadata = [
      "--repo",
      repo,
      "--title",
      tag,
      "--target",
      commit,
      "--notes",
      NOTES,
      "--latest",
    ];
    if (state.tagExists) {
      await publish([
        "api",
        "--method",
        "PATCH",
        `repos/${repo}/git/refs/tags/${tag}`,
        "--raw-field",
        `sha=${commit}`,
        "--field",
        "force=true",
        "--silent",
      ]);
    } else {
      await publish([
        "api",
        "--method",
        "POST",
        `repos/${repo}/git/refs`,
        "--raw-field",
        `ref=refs/tags/${tag}`,
        "--raw-field",
        `sha=${commit}`,
        "--silent",
      ]);
    }
    if (state.releaseExists) {
      await publish([
        "release",
        "upload",
        tag,
        ...state.assets,
        "--clobber",
        "--repo",
        repo,
      ]);
      await publish([
        "release",
        "edit",
        tag,
        ...metadata,
        "--draft=false",
        "--prerelease=false",
      ]);
    } else {
      await publish([
        "release",
        "create",
        tag,
        ...state.assets,
        ...metadata,
        "--verify-tag",
      ]);
    }
  }
  console.log(
    options.dryRun
      ? "Dry run complete; GitHub was left unchanged."
      : "Proxy releases updated.",
  );
}

export async function main(args = process.argv.slice(2)) {
  const options = parseArgs(args);
  if (options.help) {
    console.log(
      "Usage: node scripts/release.js [yukino-agent-proxy] [--dry-run]",
    );
    console.log(
      "With no project name, release the proxy. Tags, titles and assets have no version or timestamp.",
    );
    console.log(
      "--dry-run builds and inspects GitHub, then prints writes without applying them.",
    );
    return;
  }
  await release(options.names, { dryRun: options.dryRun });
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  main().catch(( error) => {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  });
}
