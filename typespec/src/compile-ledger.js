import { compile, NodeHost } from "@typespec/compiler";
import path from "node:path";

const required = "required";
const excluded = "excluded";

export class LedgerCompileError extends Error {
  constructor(code, operationID, detail = "") {
    const suffix = detail === "" ? "" : `: ${detail}`;
    super(`operation ${JSON.stringify(operationID)}: ${code}${suffix}`);
    this.name = "LedgerCompileError";
    this.code = code;
    this.operationID = operationID;
  }
}

// compileLedger is the canonical source-to-ledger compiler. TypeSpec performs
// all syntax and type/reference validation before this code inspects the
// semantic operation graph and its extension-owned @operation metadata.
export async function compileLedger(mainFile) {
  const program = await compile(NodeHost, mainFile, { noEmit: true });
  const diagnostics = program.diagnostics.filter((diagnostic) => diagnostic.severity === "error");
  if (diagnostics.length > 0) {
    throw new Error(`TypeSpec compilation failed: ${diagnostics.map((diagnostic) => diagnostic.message).sort().join("; ")}`);
  }

  const operations = collectOperations(program.getGlobalNamespaceType()).filter((operation) =>
    isProjectOperation(operation),
  );
  const metadata = operations.map(readOperationMetadata);
  validateUniqueIDs(metadata);

  return metadata
    .map(({ operationID, http, cli }) => ({ operationID, http, cli }))
    .sort((left, right) => left.operationID.localeCompare(right.operationID));
}

function isProjectOperation(operation) {
  const sourceFile = sourceFilePath(operation.node);
  if (sourceFile === undefined) {
    return false;
  }
  // TypeSpec's own operations live under node_modules. Every other loaded
  // source is an extension contract source, including sibling and parent
  // imports that the entry file composes into one ledger.
  return !sourceFile.split(path.sep).includes("node_modules");
}

function sourceFilePath(node) {
  for (let current = node; current !== undefined; current = current.parent) {
    if (current.file?.path !== undefined) {
      return current.file.path;
    }
  }
  return undefined;
}

function collectOperations(namespace, operations = []) {
  operations.push(...namespace.operations.values());
  for (const iface of namespace.interfaces.values()) {
    operations.push(...iface.operations.values());
  }
  for (const child of namespace.namespaces.values()) {
    collectOperations(child, operations);
  }
  return operations;
}

function readOperationMetadata(operation) {
  const application = operation.decorators.find((decorator) => decorator.definition?.name === "@operation");
  if (application === undefined) {
    throw new LedgerCompileError("missing TypeSpec operation metadata", operation.name);
  }

  const [operationID, httpDisposition, httpReason, cliDisposition, cliReason] = application.args.map(
    (argument) => argument.jsValue,
  );
  if (typeof operationID !== "string" || operationID.trim() === "") {
    throw new LedgerCompileError("missing operation ID", operation.name);
  }

  const http = projection(operationID, "http", httpDisposition, httpReason);
  const cli = projection(operationID, "cli", cliDisposition, cliReason);
  if (!http.included && !cli.included) {
    throw new LedgerCompileError("operation excluded from all projections", operationID);
  }
  return { operationID, http, cli };
}

function projection(operationID, name, disposition, reason) {
  if (disposition === required) {
    return { disposition: required, included: true };
  }
  if (disposition === excluded) {
    if (typeof reason !== "string" || reason.trim() === "") {
      throw new LedgerCompileError("missing exclusion reason", operationID, name);
    }
    return { disposition: excluded, included: false, reason };
  }
  if (disposition === "") {
    throw new LedgerCompileError("missing projection disposition", operationID, name);
  }
  throw new LedgerCompileError("unknown projection disposition", operationID, name);
}

function validateUniqueIDs(entries) {
  const IDs = entries.map((entry) => entry.operationID).sort();
  for (let index = 1; index < IDs.length; index += 1) {
    if (IDs[index] === IDs[index - 1]) {
      throw new LedgerCompileError("duplicate operation ID", IDs[index]);
    }
  }
}
