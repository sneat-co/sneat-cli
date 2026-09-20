import "../operation-metadata.tsp";

namespace Contactius.Shared {
  model Contact {}

  @operation("contactus.contacts.shared-list", "required", "", "required", "")
  op listContacts(): Contact[];
}
