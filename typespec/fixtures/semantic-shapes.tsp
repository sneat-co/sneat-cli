import "../operation-metadata.tsp";

model Contact {}

union ContactResult {
  one: Contact,
  many: Contact[],
}

@operation("contactus.contacts.list", "required", "", "required", "")
op listContacts(filter: Contact): Contact[];

@operation("contactus.contacts.result", "required", "", "required", "")
op getContactResult(): ContactResult;
