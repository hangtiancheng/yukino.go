// @ts-check

import assert from "node:assert/strict";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { parseArgs, release } from "./release.js";

/** @typedef {import("./release.js").ProjectName} ProjectName */
/** @typedef {import("./release.js").Runner} Runner */
/**
 * @typedef {object} FixtureOptions
 * @property {boolean} [releaseExists]
 * @property {boolean} [tagExists]
 * @property {boolean} [immutable]
 * @property {boolean} [missingCommit]
 * @property {boolean} [headChanges]
 * @property {ProjectName} [buildFailure]
 * @property {ProjectName} [emptyAsset]
 * @property {number} [lookupError]
 * @typedef {object} Invocation
 * @property {string} command
 * @property {readonly string[]} args
 */

const HEAD = "a".repeat(40);
/** @type {readonly ProjectName[]} */
const NAMES = ["yukino-agent-proxy"];
const PLATFORMS = [
  "linux-x64",
  "linux-arm64",
  "darwin-x64",
  "darwin-arm64",
  "win32-x64",
  "win32-arm64",
];

/**
 * Exercise the full release flow with a fake command runner and real temp files.
 * No test invokes GitHub or changes remote refs.
 * @param {import("node:test").TestContext} t
 * @param {FixtureOptions} [options]
 * @returns {Promise<{root: string, runner: Runner, calls: Invocation[]}>}
 */
async function fixture(t, options = {}) {
  const root = await mkdtemp(path.join(tmpdir(), "yukino-release-test-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  /** @type {Invocation[]} */
  const calls = [];
  let headReads = 0;
  /** @type {Runner} */
  const runner = async (command, args, runOptions) => {
    assert.equal(runOptions.cwd, root);
    calls.push({ command, args: [...args] });
    if (command === "git") {
      headReads++;
      return {
        code: 0,
        stdout: options.headChanges && headReads > 1 ? "b".repeat(40) : HEAD,
        stderr: "",
      };
    }
    if (command === process.execPath) {
      const script = args[0];
      assert.ok(script);
      const directory = path.dirname(script);
      const name = path.basename(directory).replaceAll("_", "-");
      if (options.buildFailure === name) throw new Error("go build failed");
      await mkdir(path.join(directory, "bin"), { recursive: true });
      for (const platform of PLATFORMS) {
        await writeFile(
          path.join(directory, "bin", `${name}-${platform}`),
          options.emptyAsset === name ? "" : "fixture executable",
        );
      }
      await writeFile(
        path.join(directory, "bin", name),
        "excluded native alias",
      );
      return { code: 0, stdout: "", stderr: "" };
    }
    assert.equal(command, "gh");
    if (args[0] === "repo")
      return { code: 0, stdout: "owner/repo\n", stderr: "" };
    if (!args.includes("--include")) return { code: 0, stdout: "", stderr: "" };
    const endpoint = args[1];
    assert.ok(endpoint);
    let status = 200;
    /** @type {Record<string, unknown>} */
    let body = {};
    if (endpoint.includes("/commits/")) {
      status = options.missingCommit ? 404 : 200;
      body = { sha: HEAD };
    } else if (endpoint.includes("/releases/tags/")) {
      status =
        options.lookupError ?? (options.releaseExists === false ? 404 : 200);
      body = {
        tag_name: endpoint.split("/").at(-1),
        immutable: options.immutable ?? false,
      };
    } else if (endpoint.includes("/git/ref/tags/")) {
      status = options.tagExists === false ? 404 : 200;
      body = { ref: `refs/tags/${endpoint.split("/").at(-1)}` };
    } else {
      assert.fail(`Unexpected GitHub lookup: ${endpoint}`);
    }
    return {
      code: status >= 400 ? 1 : 0,
      stdout: `HTTP/2.0 ${status}\r\nContent-Type: application/json\r\n\r\n${JSON.stringify(body)}\n`,
      stderr: status >= 400 ? `gh: HTTP ${status}` : "",
    };
  };
  return { root, runner, calls };
}

/**
 * @param {readonly Invocation[]} calls
 * @returns {Invocation[]}
 */
function mutations(calls) {
  return calls.filter(
    (call) =>
      call.command === "gh" &&
      (call.args[0] === "release" || call.args.includes("--method")),
  );
}

test("CLI defaults to the fixed release and accepts an explicit project", () => {
  assert.deepEqual(parseArgs([]), {
    help: false,
    dryRun: false,
    names: NAMES,
  });
  assert.deepEqual(parseArgs(["yukino-agent-proxy", "--dry-run"]).names, [
    "yukino-agent-proxy",
  ]);
  assert.equal(parseArgs(["--help"]).help, true);
  assert.deepEqual(
    parseArgs(["yukino-agent-proxy", "yukino-agent-proxy"]).names,
    ["yukino-agent-proxy"],
  );
  assert.throws(() => parseArgs(["v1.0.0"]), /Unknown argument/);
});

test("an existing release replaces six fixed assets, updates the tag and refreshes metadata", async (t) => {
  const f = await fixture(t);
  await release(NAMES, f);
  const writes = mutations(f.calls);
  const firstWrite = f.calls.findIndex((call) => writes.includes(call));
  const builds = f.calls
    .map((call, i) => (call.command === process.execPath ? i : -1))
    .filter((i) => i >= 0);
  assert.equal(builds.length, 1);
  assert.ok(builds.every((i) => i < firstWrite));
  assert.equal(writes.length, 3);
  for (const name of NAMES) {
    const upload = writes.find(
      (call) => call.args[1] === "upload" && call.args[2] === name,
    );
    assert.ok(upload);
    assert.ok(upload.args.includes("--clobber"));
    assert.deepEqual(
      upload.args.slice(3, 9).map((asset) => path.basename(asset)),
      PLATFORMS.map((platform) => `${name}-${platform}`),
    );
    const edit = writes.find(
      (call) => call.args[1] === "edit" && call.args[2] === name,
    );
    assert.ok(edit);
    assert.equal(edit.args[edit.args.indexOf("--title") + 1], name);
    assert.equal(edit.args[edit.args.indexOf("--target") + 1], HEAD);
    const tag = writes.find((call) =>
      call.args.includes(`repos/owner/repo/git/refs/tags/${name}`),
    );
    assert.ok(tag?.args.includes("PATCH"));
    assert.ok(tag?.args.includes(`sha=${HEAD}`));
    assert.ok(tag?.args.includes("force=true"));
  }
});

test("a new release creates the fixed tag and requires it during release creation", async (t) => {
  const f = await fixture(t, { releaseExists: false, tagExists: false });
  await release(NAMES, f);
  const writes = mutations(f.calls);
  assert.equal(writes.length, 2);
  assert.equal(writes.filter((call) => call.args.includes("POST")).length, 1);
  for (const name of NAMES) {
    const created = writes.find(
      (call) => call.args[1] === "create" && call.args[2] === name,
    );
    assert.ok(created?.args.includes("--verify-tag"));
    assert.ok(
      writes.some((call) => call.args.includes(`ref=refs/tags/${name}`)),
    );
  }
});

test("an existing tag without a release is moved before creating the release", async (t) => {
  const f = await fixture(t, { releaseExists: false });
  await release(["yukino-agent-proxy"], f);
  const writes = mutations(f.calls);
  assert.equal(writes.length, 2);
  assert.ok(writes[0]?.args.includes("PATCH"));
  assert.equal(writes[1]?.args[1], "create");
});

test("dry run builds selected artifacts without any remote writes", async (t) => {
  const f = await fixture(t);
  await release(["yukino-agent-proxy"], { ...f, dryRun: true });
  assert.equal(
    f.calls.filter((call) => call.command === process.execPath).length,
    1,
  );
  assert.equal(mutations(f.calls).length, 0);
});

test("a build failure leaves the release untouched", async (t) => {
  const f = await fixture(t, { buildFailure: "yukino-agent-proxy" });
  await assert.rejects(release(NAMES, f), /go build failed/);
  assert.equal(mutations(f.calls).length, 0);
});

test("empty executables cannot be published", async (t) => {
  const f = await fixture(t, { emptyAsset: "yukino-agent-proxy" });
  await assert.rejects(release(NAMES, f), /invalid release executable/);
  assert.equal(mutations(f.calls).length, 0);
});

for (const status of [401, 403, 500]) {
  test(`HTTP ${status} is not mistaken for a missing release`, async (t) => {
    const f = await fixture(t, { lookupError: status });
    await assert.rejects(release(NAMES, f), new RegExp(`HTTP ${status}`));
    assert.equal(mutations(f.calls).length, 0);
    assert.equal(
      f.calls.filter((call) => call.command === process.execPath).length,
      0,
    );
  });
}

test("HEAD must exist remotely and stay constant throughout the build", async (t) => {
  for (const options of [{ missingCommit: true }, { headChanges: true }]) {
    const f = await fixture(t, options);
    await assert.rejects(release(NAMES, f), /push the commit|HEAD changed/);
    assert.equal(mutations(f.calls).length, 0);
  }
});

test("immutable releases fail before building or changing remote refs", async (t) => {
  const f = await fixture(t, { immutable: true });
  await assert.rejects(release(NAMES, f), /immutable/);
  assert.equal(mutations(f.calls).length, 0);
});
