import "../operation-metadata.tsp";

model Contact {}

@operation("contactus.contacts.list", "required", "", "required", "")
op listContacts(): Contact;

@operation("contactus.contacts.list", "required", "", "required", "")
op getContact(): Contact;
