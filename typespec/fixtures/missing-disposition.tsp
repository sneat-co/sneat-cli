import "../operation-metadata.tsp";

model ContactList {}

@operation("contactus.contacts.list", "", "", "required", "")
op listContacts(): ContactList;
