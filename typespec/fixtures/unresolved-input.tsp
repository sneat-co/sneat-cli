import "../operation-metadata.tsp";

model Contact {}

@operation("contactus.contacts.create", "required", "", "required", "")
op createContact(input: MissingInput): Contact;
