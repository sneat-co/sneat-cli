import "../operation-metadata.tsp";
import "@typespec/http";
import "@typespec/openapi";

using TypeSpec.Http;
using TypeSpec.OpenAPI;

@service(#{title: "Projection fixture"})
namespace ProjectionFixture {
  model Contact {}
  model ContactList {}
  model ContactListInput {}

  @operation("contactus.contacts.list", "required", "", "required", "")
  @operationId("contactus.contacts.list")
  @route("/contacts")
  @get
  @cli("contact list", "contacts,ls", "json,yaml", "collection", "read", "safe")
  op listContacts(input: ContactListInput): ContactList;

  @operation("contactus.contacts.get", "required", "", "excluded", "interactive card details are not an agent command")
  @operationId("contactus.contacts.get")
  @route("/contacts/{id}")
  @get
  op getContact(@path id: string): Contact;
}

namespace LocalConfig {
  model LocalConfig {}

  @operation("sneat.config.clear-current-space", "excluded", "this command changes only local CLI configuration", "required", "")
  @cli("config clear-current-space", "", "json,yaml", "single", "mutation", "idempotent")
  op clearCurrentSpace(): LocalConfig;
}
