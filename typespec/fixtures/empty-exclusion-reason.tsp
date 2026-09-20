import "../operation-metadata.tsp";

model ContactList {}

@operation("contactus.contacts.list", "required", "", "excluded", "")
op listContacts(): ContactList;
