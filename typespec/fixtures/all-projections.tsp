import "../operation-metadata.tsp";

model Contact {}
model ContactList {}
model LocalConfig {}

@operation("contactus.contacts.list", "required", "", "required", "")
op listContacts(): ContactList;

@operation("contactus.contacts.get", "required", "", "excluded", "interactive card details are not an agent command")
op getContact(): Contact;

@operation("sneat.config.clear-current-space", "excluded", "this command changes only local CLI configuration", "required", "")
op clearCurrentSpace(): LocalConfig;
