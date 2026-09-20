import path from "node:path";

import { checkProjections, emitProjections } from "./projection.js";

const [mode, source, output] = process.argv.slice(2);
const checking = mode === "--check";
const mainFile = path.resolve(checking ? source : mode);
const outputDir = path.resolve(checking ? output : source);

if ((checking && output === undefined) || (!checking && source === undefined)) {
  throw new Error("usage: generate-projections.js [--check] <main-file> <output-directory>");
}

if (checking) {
  await checkProjections(mainFile, outputDir);
} else {
  await emitProjections(mainFile, outputDir);
}
