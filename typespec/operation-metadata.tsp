import "./operation-metadata.js";
using TypeSpec.Reflection;

// Extension-owned metadata that binds one stable operation identity to both
// independently selected projections.
extern dec operation(
  target: Operation,
  id: valueof string,
  http: valueof string,
  httpReason: valueof string,
  cli: valueof string,
  cliReason: valueof string,
);

// CLI metadata is declared only by operations included in the CLI projection.
// Its compact source form is normalized into arrays and typed manifest fields by
// the projection compiler, which keeps generated consumers from parsing prose.
extern dec cli(
  target: Operation,
  commandPath: valueof string,
  aliases: valueof string,
  formats: valueof string,
  collection: valueof string,
  mutation: valueof string,
  idempotency: valueof string,
);
