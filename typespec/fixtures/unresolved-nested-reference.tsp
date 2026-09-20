import "../operation-metadata.tsp";

model Contact {
  nested: MissingNested;
}

@operation("contactus.contacts.list", "required", "", "required", "")
op listContacts(): Contact;
