import "../operation-metadata.tsp";

model ContactList {}

@operation("contactus.contacts.list", "excluded", "not exposed over HTTP", "excluded", "not exposed as a CLI command")
op listContacts(): ContactList;
