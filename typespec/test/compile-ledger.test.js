import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";

import { compileLedger, LedgerCompileError } from "../src/compile-ledger.js";

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
