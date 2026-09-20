import "../operation-metadata.tsp";
import "@typespec/http";
import "@typespec/openapi";

using TypeSpec.Http;
using TypeSpec.OpenAPI;

@service
namespace DuplicateCommandPath {
  model Contact {}

  @operation("contactus.contacts.list", "required", "", "required", "")
  @operationId("contactus.contacts.list")
  @route("/contacts")
  @get
  @cli("contact   list", "", "json", "collection", "read", "safe")
  op listContacts(): Contact;

  @operation("contactus.contacts.get", "required", "", "required", "")
  @operationId("contactus.contacts.get")
  @route("/contacts/{id}")
  @get
  @cli("contact get", "", "json", "single", "read", "safe")
  op getContact(@path id: string): Contact;

  @operation("contactus.contacts.find", "required", "", "required", "")
  @operationId("contactus.contacts.find")
  @route("/contacts/find")
  @get
  @cli("contact list", "", "json", "collection", "read", "safe")
  op findContacts(): Contact;
}
