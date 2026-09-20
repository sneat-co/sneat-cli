import "../operation-metadata.tsp";
import "@typespec/http";
import "@typespec/openapi";

using TypeSpec.Http;
using TypeSpec.OpenAPI;

@service
namespace ExcludedHTTPLeak {
  model LocalConfig {}

  @operation("sneat.config.clear-current-space", "excluded", "this command changes only local CLI configuration", "required", "")
  @operationId("sneat.config.clear-current-space")
  @route("/local/config/current-space")
  @get
  @cli("config clear-current-space", "", "json", "single", "mutation", "idempotent")
  op clearCurrentSpace(): LocalConfig;
}
