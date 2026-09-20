import "../operation-metadata.tsp";

namespace Contactius {
  model Contact {}

  @operation("contactus.contacts.get", "required", "", "required", "")
  op getContact(): Contact;

  interface Contacts {
    @operation("contactus.contacts.list", "required", "", "required", "")
    listContacts(): Contact[];
  }
}
