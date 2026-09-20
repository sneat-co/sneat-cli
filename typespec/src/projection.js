import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

import { getOpenAPI3 } from "@typespec/openapi3";

import { compileOperationModel } from "./compile-ledger.js";

const manifestVersion = "1.0.0";
const openapiFilename = "openapi.json";
const manifestFilename = "cli-capabilities.json";

export class ProjectionCompileError extends Error {
  constructor(code, operationID, detail = "") {
    const suffix = detail === "" ? "" : `: ${detail}`;
    super(`operation ${JSON.stringify(operationID)}: ${code}${suffix}`);
    this.name = "ProjectionCompileError";
    this.code = code;
    this.operationID = operationID;
  }
}

// emitProjections turns the canonical semantic operation model into both
// checked artifacts. The OpenAPI document comes from the pinned TypeSpec
// emitter; the CLI projection is the deterministic, machine-readable manifest
// consumed by CLI discovery surfaces.
export async function emitProjections(mainFile, outputDir) {
  const model = await compileOperationModel(mainFile);
  const capabilities = model.operations
    .filter((entry) => entry.cli.included)
    .map(readCapability)
    .sort((left, right) => left.operationID.localeCompare(right.operationID));
  validateUniqueCommandPaths(capabilities);

  const openapi = await emitOpenAPI(model);
  const httpMappings = collectHTTPMappings(openapi);
  validateHTTPProjection(model.operations, httpMappings);

  for (const capability of capabilities) {
    capability.http = httpMappings.get(capability.operationID);
  }
  const manifest = { version: manifestVersion, operations: capabilities };

  await mkdir(outputDir, { recursive: true });
  const openapiPath = path.join(outputDir, openapiFilename);
  const manifestPath = path.join(outputDir, manifestFilename);
  await Promise.all([
    writeFile(openapiPath, canonicalJSON(openapi)),
    writeFile(manifestPath, canonicalJSON(manifest)),
  ]);
  return {
    openapi: artifact(openapiPath),
    manifest: artifact(manifestPath),
  };
}

// checkProjections is the non-mutating CI gate. It regenerates into an
// isolated directory and compares exact bytes with the checked artifacts.
export async function checkProjections(mainFile, outputDir) {
  const temporaryDir = await mkdtemp(path.join(os.tmpdir(), "sneat-projection-check-"));
  try {
    await emitProjections(mainFile, temporaryDir);
    for (const filename of [openapiFilename, manifestFilename]) {
      const [expected, actual] = await Promise.all([
        readFile(path.join(temporaryDir, filename), "utf8"),
        readFile(path.join(outputDir, filename), "utf8").catch(() => undefined),
      ]);
      if (actual !== expected) {
        throw new Error(`generated projection artifact differs: ${filename}`);
      }
    }
  } finally {
    await rm(temporaryDir, { recursive: true, force: true });
  }
}

export async function loadCapabilityManifest(manifestPath) {
  const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
  if (manifest.version !== manifestVersion || !Array.isArray(manifest.operations)) {
    throw new Error(`invalid CLI capability manifest at ${manifestPath}`);
  }
  const operations = new Map(manifest.operations.map((operation) => [operation.operationID, operation]));
  return {
    version: manifest.version,
    operations: manifest.operations,
    byOperationID(operationID) {
      const operation = operations.get(operationID);
      if (operation === undefined) {
        throw new Error(`unknown CLI capability operation ${JSON.stringify(operationID)}`);
      }
      return operation;
    },
    schema(operationID) {
      const operation = this.byOperationID(operationID);
      return { inputs: operation.inputs, result: operation.result };
    },
    helpMetadata(operationID) {
      const operation = this.byOperationID(operationID);
      return {
        aliases: operation.aliases,
        commandPath: operation.commandPath,
        formats: operation.formats,
      };
    },
  };
}

async function emitOpenAPI(model) {
  const documents = await getOpenAPI3(model.program, {
    "file-type": "json",
    "new-line": "lf",
    "omit-unreachable-types": true,
    "openapi-versions": ["3.0.0"],
    "operation-id-strategy": "explicit-only",
  });
  const diagnostics = documents.flatMap((record) =>
    record.versioned ? record.versions.flatMap((version) => version.diagnostics) : record.diagnostics,
  );
  const errors = diagnostics.filter((diagnostic) => diagnostic.severity === "error");
  if (errors.length > 0) {
    throw new ProjectionCompileError(
      "OpenAPI emission failed",
      "OpenAPI",
      errors.map((diagnostic) => diagnostic.message).sort().join("; "),
    );
  }
  if (documents.length !== 1 || documents[0].versioned) {
    throw new ProjectionCompileError("expected one unversioned OpenAPI service", "OpenAPI");
  }
  const openapi = documents[0].document;
  assertReferencesResolve(openapi, openapi);
  return openapi;
}

function readCapability(entry) {
  const application = entry.operation.decorators.find((decorator) => decorator.definition?.name === "@cli");
  if (application === undefined) {
    throw new ProjectionCompileError("missing CLI projection metadata", entry.operationID);
  }
  const [commandPath, aliases, formats, collection, mutation, idempotency] = application.args.map(
    (argument) => argument.jsValue,
  );
  if (typeof commandPath !== "string" || commandPath.trim() === "") {
    throw new ProjectionCompileError("missing CLI command path", entry.operationID);
  }
  if (typeof formats !== "string" || formats.trim() === "") {
    throw new ProjectionCompileError("missing CLI formats", entry.operationID);
  }
  for (const [name, value] of [
    ["collection", collection],
    ["mutation", mutation],
    ["idempotency", idempotency],
  ]) {
    if (typeof value !== "string" || value.trim() === "") {
      throw new ProjectionCompileError(`missing CLI ${name} classification`, entry.operationID);
    }
  }
  return {
    operationID: entry.operationID,
    commandPath: normalizeCommandPath(commandPath),
    aliases: csv(aliases, entry.operationID, "aliases").map(normalizeCommandPath),
    inputs: inputSchemas(entry.operation),
    result: typeName(entry.operation.returnType),
    formats: csv(formats, entry.operationID, "formats"),
    collection: collection.trim(),
    pagination: "unspecified",
    ordering: "unspecified",
    mutation: mutation.trim(),
    idempotency: idempotency.trim(),
  };
}

function inputSchemas(operation) {
  return [...operation.parameters.properties.values()]
    .map((property) => ({
      name: property.name,
      required: !property.optional,
      schema: typeName(property.type),
    }))
    .sort((left, right) => left.name.localeCompare(right.name));
}

function csv(value, operationID, name) {
  if (typeof value !== "string") {
    throw new ProjectionCompileError(`invalid CLI ${name}`, operationID);
  }
  if (value.trim() === "") {
    return [];
  }
  const values = value.split(",").map((item) => item.trim());
  if (values.some((item) => item === "")) {
    throw new ProjectionCompileError(`invalid CLI ${name}`, operationID);
  }
  return [...new Set(values)].sort((left, right) => left.localeCompare(right));
}

function typeName(type) {
  return type.name ?? type.kind;
}

function validateUniqueCommandPaths(capabilities) {
  const operationByPath = new Map();
  const operationByName = new Map();
  for (const capability of capabilities) {
    if (operationByPath.has(capability.commandPath)) {
      throw new ProjectionCompileError("duplicate command path", capability.operationID, capability.commandPath);
    }
    operationByPath.set(capability.commandPath, capability.operationID);
    for (const name of [capability.commandPath, ...capability.aliases]) {
      if (operationByName.has(name)) {
        throw new ProjectionCompileError("duplicate CLI command name", capability.operationID, name);
      }
      operationByName.set(name, capability.operationID);
    }
  }
}

function normalizeCommandPath(value) {
  return value.trim().split(/\s+/).join(" ");
}

function collectHTTPMappings(openapi) {
  const mappings = new Map();
  for (const [route, pathItem] of Object.entries(openapi.paths ?? {})) {
    for (const [method, operation] of Object.entries(pathItem)) {
      if (!httpMethods.has(method)) {
        continue;
      }
      if (operation?.operationId === undefined) {
        throw new ProjectionCompileError("OpenAPI operation is missing a stable operation ID", "OpenAPI", `${method.toUpperCase()} ${route}`);
      }
      if (mappings.has(operation.operationId)) {
        throw new ProjectionCompileError("duplicate OpenAPI operation ID", operation.operationId);
      }
      mappings.set(operation.operationId, { method: method.toUpperCase(), path: route, operationID: operation.operationId });
    }
  }
  return mappings;
}

function validateHTTPProjection(entries, mappings) {
  const expected = new Set(entries.filter((entry) => entry.http.included).map((entry) => entry.operationID));
  for (const entry of entries) {
    const emitted = mappings.has(entry.operationID);
    if (entry.http.included && !emitted) {
      throw new ProjectionCompileError("missing required HTTP operation", entry.operationID);
    }
    if (!entry.http.included && emitted) {
      throw new ProjectionCompileError("excluded HTTP operation emitted", entry.operationID);
    }
  }
  for (const operationID of mappings.keys()) {
    if (!expected.has(operationID)) {
      throw new ProjectionCompileError("OpenAPI operation is not in the HTTP projection ledger", operationID);
    }
  }
}

function assertReferencesResolve(value, document, pathToValue = "#") {
  if (Array.isArray(value)) {
    value.forEach((item, index) => assertReferencesResolve(item, document, `${pathToValue}/${index}`));
    return;
  }
  if (value === null || typeof value !== "object") {
    return;
  }
  if (typeof value.$ref === "string") {
    const target = resolveReference(document, value.$ref);
    if (target === undefined) {
      throw new ProjectionCompileError("unresolved OpenAPI reference", "OpenAPI", `${pathToValue} -> ${value.$ref}`);
    }
  }
  Object.entries(value).forEach(([key, item]) => assertReferencesResolve(item, document, `${pathToValue}/${key}`));
}

function resolveReference(document, reference) {
  if (!reference.startsWith("#/")) {
    return undefined;
  }
  return reference
    .slice(2)
    .split("/")
    .map((part) => part.replaceAll("~1", "/").replaceAll("~0", "~"))
    .reduce((target, part) => target?.[part], document);
}

function canonicalJSON(value) {
  return `${JSON.stringify(sortObject(value), null, 2)}\n`;
}

function sortObject(value) {
  if (Array.isArray(value)) {
    return value.map(sortObject);
  }
  if (value === null || typeof value !== "object") {
    return value;
  }
  return Object.fromEntries(Object.keys(value).sort().map((key) => [key, sortObject(value[key])]));
}

function artifact(artifactPath) {
  return { path: artifactPath, read: () => readFile(artifactPath, "utf8") };
}

const httpMethods = new Set(["delete", "get", "head", "options", "patch", "post", "put", "trace"]);
