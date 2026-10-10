#!/usr/bin/env node
// @ts-check

import { spawn } from "node:child_process";
import { link, mkdir, rename, rm, symlink } from "node:fs/promises";
import { availableParallelism } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";


const root = path.dirname(fileURLToPath(import.meta.url));
const bin = path.join(root, "bin");
const binaryName = "yukino-agent-proxy";

const targets = [
  { platform: "linux", arch: "x64", goos: "linux", goarch: "amd64" },
  { platform: "linux", arch: "arm64", goos: "linux", goarch: "arm64" },
  { platform: "darwin", arch: "x64", goos: "darwin", goarch: "amd64" },
  { platform: "darwin", arch: "arm64", goos: "darwin", goarch: "arm64" },
  { platform: "win32", arch: "x64", goos: "windows", goarch: "amd64" },
  { platform: "win32", arch: "arm64", goos: "windows", goarch: "arm64" },
];

function hasCode(error, code) {
  return error instanceof Error && "code" in error && error.code === code;
}

function artifactName(target) {
  return `${binaryName}-${target.platform}-${target.arch}`;
}

async function build(target) {
  const name = artifactName(target);
  const output = path.join(bin, name);
  const temporary = path.join(bin, `.${name}.${process.pid}.tmp`);
  console.log(`Building ${name}`);
  try {
    await new Promise((resolve, reject) => {
      const child = spawn(
        "go",
        [
          "build",
          "-trimpath",
          "-ldflags=-s -w",
          "-o",
          temporary,
          "./cmd/agent_proxy",
        ],
        {
          cwd: root,
          env: {
            ...process.env,
            CGO_ENABLED: "0",
            GOOS: target.goos,
            GOARCH: target.goarch,
            GOWORK: process.env.GOWORK ?? "off",
            GOMAXPROCS: process.env.GOMAXPROCS ?? "2",
          },
          stdio: "inherit",
        },
      );
      child.once("error", reject);
      child.once("close", (code, signal) => {
        if (code === 0) resolve(undefined);
        else
          reject(
            new Error(
              `${name}: go build ${signal ? `terminated by ${signal}` : `exited with code ${code}`}`,
            ),
          );
      });
    });
    await rename(temporary, output);
    console.log(`Built ${name}`);
  } finally {
    await rm(temporary, { force: true });
  }
}

async function createNativeLink(native) {
  const name = artifactName(native);
  const output = path.join(bin, binaryName);
  const temporary = path.join(bin, `.${binaryName}.${process.pid}.link`);
  try {
    try {
      await symlink(name, temporary, "file");
    } catch (error) {
      if (
        process.platform !== "win32" ||
        (!hasCode(error, "EPERM") && !hasCode(error, "EACCES"))
      )
        throw error;
      await link(path.join(bin, name), temporary);
    }
    await rename(temporary, output);
    console.log(`Linked ${binaryName} -> ${name}`);
  } finally {
    await rm(temporary, { force: true });
  }
}

async function main() {
  const native = targets.find(
    (target) =>
      target.platform === process.platform && target.arch === process.arch,
  );
  if (!native)
    throw new Error(
      `Unsupported host: ${process.platform}/${process.arch}; no native link target.`,
    );
  const requested = process.env.BUILD_CONCURRENCY;
  const concurrency =
    requested === undefined
      ? Math.min(3, availableParallelism())
      : Number(requested);
  if (!Number.isSafeInteger(concurrency) || concurrency < 1)
    throw new Error("BUILD_CONCURRENCY must be a positive integer.");
  await mkdir(bin, { recursive: true });
  let next = 0;
  const failures = [];
  async function worker() {
    for (;;) {
      const target = targets[next++];
      if (!target) return;
      try {
        await build(target);
      } catch (error) {
        failures.push(
          error instanceof Error ? error : new Error(String(error)),
        );
      }
    }
  }
  await Promise.all(
    Array.from({ length: Math.min(concurrency, targets.length) }, worker),
  );
  if (failures.length)
    throw new AggregateError(
      failures,
      `${failures.length} target build(s) failed; native link was not changed.`,
    );
  await createNativeLink(native);
}

main().catch((error) => {
  if (error instanceof AggregateError) {
    for (const failure of error.errors)
      console.error(
        failure instanceof Error ? failure.message : String(failure),
      );
  }
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
});
