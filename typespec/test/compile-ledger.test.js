import assert from "node:assert/strict";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { compileLedger, LedgerCompileError } from "../src/compile-ledger.js";
import {
  emitProjections,
  checkProjections,
  loadCapabilityManifest,
  ProjectionCompileError,
} from "../src/projection.js";

const fixtures = path.resolve("fixtures");

test("compiles HTTP-and-CLI, HTTP-only, and CLI-only operations into one ledger", async () => {
  assert.deepEqual(await compileLedger(path.join(fixtures, "all-projections.tsp")), [
    {
      operationID: "contactus.contacts.get",
      http: { disposition: "required", included: true },
      cli: { disposition: "excluded", included: false, reason: "interactive card details are not an agent command" },
    },
    {
      operationID: "contactus.contacts.list",
      http: { disposition: "required", included: true },
      cli: { disposition: "required", included: true },
    },
    {
      operationID: "sneat.config.clear-current-space",
      http: { disposition: "excluded", included: false, reason: "this command changes only local CLI configuration" },
      cli: { disposition: "required", included: true },
    },
  ]);
});

test("enumerates array and union operations from the TypeSpec semantic model", async () => {
  const ledger = await compileLedger(path.join(fixtures, "semantic-shapes.tsp"));
  assert.deepEqual(
    ledger.map((entry) => entry.operationID),
    ["contactus.contacts.list", "contactus.contacts.result"],
  );
});

test("enumerates namespace and interface operations from the TypeSpec semantic model", async () => {
  const ledger = await compileLedger(path.join(fixtures, "namespace-and-interface.tsp"));
  assert.deepEqual(
    ledger.map((entry) => entry.operationID),
    ["contactus.contacts.get", "contactus.contacts.list"],
  );
});

test("enumerates operations imported from a sibling extension source", async () => {
  const ledger = await compileLedger(path.join(fixtures, "imports", "main.tsp"));
  assert.deepEqual(
    ledger.map((entry) => entry.operationID),
    ["contactus.contacts.shared-list"],
  );
});

test("excludes compiler-library operations when the entry is at the project root", async () => {
  assert.deepEqual(await compileLedger(path.resolve("operation-metadata.tsp")), []);
});

for (const [file, code] of [
  ["missing-disposition.tsp", "missing projection disposition"],
  ["empty-exclusion-reason.tsp", "missing exclusion reason"],
  ["duplicate-id.tsp", "duplicate operation ID"],
  ["both-excluded.tsp", "operation excluded from all projections"],
  ["undecorated-operation.tsp", "missing TypeSpec operation metadata"],
]) {
  test(`rejects ${file}`, async () => {
    await assert.rejects(compileLedger(path.join(fixtures, file)), (error) => {
      assert.ok(error instanceof LedgerCompileError);
      assert.equal(error.code, code);
      assert.match(error.message, /contactus\.contacts\.list|listContacts/);
      return true;
    });
  });
}

test("rejects unresolved TypeSpec references before constructing a ledger", async () => {
  await assert.rejects(compileLedger(path.join(fixtures, "unresolved-reference.tsp")), /TypeSpec compilation failed/);
});

for (const file of ["unresolved-input.tsp", "unresolved-nested-reference.tsp"]) {
  test(`rejects ${file} before constructing a ledger`, async () => {
    await assert.rejects(compileLedger(path.join(fixtures, file)), /TypeSpec compilation failed/);
  });
}

test("emits byte-identical OpenAPI and CLI capability contracts from the ledger", async (t) => {
  const outputRoot = await mkdtemp(path.join(os.tmpdir(), "sneat-projections-"));
  t.after(() => rm(outputRoot, { recursive: true, force: true }));
  const outputs = await Promise.all(
    ["first", "second"].map(async (name) => {
      const outputDir = path.join(outputRoot, name);
      return emitProjections(path.join(fixtures, "projection-contract.tsp"), outputDir);
    }),
  );

  const [first, second] = outputs;
  assert.equal(await first.openapi.read(), await second.openapi.read());
  assert.equal(await first.manifest.read(), await second.manifest.read());

  const openapi = JSON.parse(await first.openapi.read());
  assert.deepEqual(
    Object.values(openapi.paths)
      .flatMap((pathItem) =>
        Object.entries(pathItem)
          .filter(([method]) => ["delete", "get", "head", "options", "patch", "post", "put", "trace"].includes(method))
          .map(([, operation]) => operation),
      )
      .map((operation) => operation.operationId)
      .sort(),
    ["contactus.contacts.get", "contactus.contacts.list"],
  );

  const manifest = await loadCapabilityManifest(first.manifest.path);
  assert.equal(manifest.version, "1.0.0");
  assert.deepEqual(
    manifest.operations.map((operation) => operation.operationID),
    ["contactus.contacts.list", "sneat.config.clear-current-space"],
  );
  assert.equal(manifest.byOperationID("contactus.contacts.list").commandPath, "contact list");
  assert.deepEqual(manifest.helpMetadata("contactus.contacts.list"), {
    aliases: ["contacts", "ls"],
    commandPath: "contact list",
    formats: ["json", "yaml"],
  });
  assert.deepEqual(manifest.schema("contactus.contacts.list"), {
    inputs: [{ name: "input", required: true, schema: "ContactListInput" }],
    result: "ContactList",
  });
});

test("rejects duplicate CLI command paths before emitting contracts", async () => {
  await assert.rejects(
    emitProjections(path.join(fixtures, "duplicate-command-path.tsp"), "/tmp/unused"),
    (error) => error instanceof ProjectionCompileError && error.code === "duplicate command path",
  );
});

test("checks committed projection artifacts against a fresh deterministic generation", async (t) => {
  const outputDir = await mkdtemp(path.join(os.tmpdir(), "sneat-projections-check-"));
  t.after(() => rm(outputDir, { recursive: true, force: true }));
  const fixture = path.join(fixtures, "projection-contract.tsp");

  await emitProjections(fixture, outputDir);
  await checkProjections(fixture, outputDir);
  await writeFile(path.join(outputDir, "cli-capabilities.json"), "{}\n");
  await assert.rejects(checkProjections(fixture, outputDir), /generated projection artifact differs/);
});

test("rejects an HTTP-excluded operation that leaks into OpenAPI", async () => {
  await assert.rejects(
    emitProjections(path.join(fixtures, "excluded-http-leak.tsp"), "/tmp/unused"),
    (error) => error instanceof ProjectionCompileError && error.code === "excluded HTTP operation emitted",
  );
});

for (const [file, error] of [
  ["duplicate-id.tsp", /duplicate operation ID/],
  ["both-excluded.tsp", /operation excluded from all projections/],
  ["unresolved-reference.tsp", /TypeSpec compilation failed/],
]) {
  test(`fails projection emission for ${file}`, async () => {
    await assert.rejects(emitProjections(path.join(fixtures, file), "/tmp/unused"), error);
  });
}
