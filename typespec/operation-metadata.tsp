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
