/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

// Fixture: a contacts app that exercises the mock data store — a collection
// created, edited and deleted across screens, a shared value, the fixed date,
// form validation, params, roles and display states.

import {
  Breadcrumbs,
  Button,
  Detail,
  EmptyState,
  Field,
  Form,
  Heading,
  Navigation,
  Screen,
  Table,
  defineApp,
  useCollection,
  useDisplayState,
  useNav,
  useParams,
  useRole,
  useToday,
  useValue,
} from "@wso2/prototype-kit";

interface Contact {
  id: string;
  name: string;
  email: string;
  added: string;
}

const nav = (
  <Navigation
    id="nav.main"
    layout="top"
    items={[
      { id: "nav.contacts", label: "Contacts", to: "screen.contacts" },
      { id: "nav.settings", label: "Settings", to: "screen.settings" },
    ]}
  />
);

/** The contact the params name, else the first one. */
function useCurrentContact() {
  const { contact: id } = useParams();
  const contacts = useCollection<Contact>("contacts");
  return { contacts, contact: (id === undefined ? undefined : contacts.get(id)) ?? contacts.items[0] };
}

function ContactList() {
  const role = useRole();
  const state = useDisplayState();
  const contacts = useCollection<Contact>("contacts");
  const [company] = useValue<string>("company");
  const rows = state === "state.empty" ? [] : contacts.items;
  return (
    <Screen nav={nav}>
      <Heading
        id="heading.contacts"
        text={`${company} contacts`}
        actions={role === "editor" ? <Button id="btn.new" label="New contact" emphasis="primary" to="screen.new" /> : undefined}
      />
      <Table
        id="table.contacts"
        columns={["Name", "Email", "Added"]}
        rows={rows.map((c) => ({ id: `row.${c.id}`, cells: [c.name, c.email, c.added], to: "screen.contact", params: { contact: c.id } }))}
        empty={<EmptyState id="empty.contacts" title="No contacts yet" text="Contacts you add appear here." />}
      />
    </Screen>
  );
}

function ContactDetail() {
  const role = useRole();
  const nav_ = useNav();
  const { contacts, contact } = useCurrentContact();
  if (!contact) {
    return (
      <Screen nav={nav}>
        <EmptyState id="empty.contact" title="No contact" text="There are no contacts to show." />
      </Screen>
    );
  }
  const remove = () => {
    contacts.remove(contact.id);
    nav_.go("screen.contacts");
  };
  return (
    <Screen nav={nav}>
      <Breadcrumbs
        id="crumbs.contact"
        items={[
          { id: "crumb.contacts", label: "Contacts", to: "screen.contacts" },
          { id: "crumb.contact", label: contact.name },
        ]}
      />
      <Heading
        id="heading.contact"
        text={contact.name}
        actions={
          role === "editor" ? (
            <>
              <Button id="btn.edit" label="Edit" to="screen.edit" params={{ contact: contact.id }} />
              <Button id="btn.delete" label="Delete" emphasis="danger" onPress={remove} />
            </>
          ) : undefined
        }
      />
      <Detail
        id="detail.contact"
        fields={[
          { label: "Id", value: contact.id },
          { label: "Email", value: contact.email },
          { label: "Added", value: contact.added },
        ]}
      />
    </Screen>
  );
}

const EMAIL = "[^@\\s]+@[^@\\s]+";

function NewContact() {
  const contacts = useCollection<Contact>("contacts");
  const today = useToday();
  const nav_ = useNav();
  return (
    <Screen nav={nav}>
      <Heading id="heading.new" text="New contact" />
      <Form
        id="form.new"
        onSubmit={(values) => {
          const id = contacts.create({ name: values["name"] ?? "", email: values["email"] ?? "", added: today });
          nav_.go("screen.contact", { contact: id });
        }}
        actions={<Button id="btn.save" label="Save" emphasis="primary" submit />}
      >
        <Field id="field.name" name="name" label="Name" required />
        <Field id="field.email" name="email" label="Email" required pattern={EMAIL} patternMessage="Enter an email address" />
      </Form>
    </Screen>
  );
}

function EditContact() {
  const nav_ = useNav();
  const { contacts, contact } = useCurrentContact();
  if (!contact) {
    return (
      <Screen nav={nav}>
        <EmptyState id="empty.edit" title="No contact" text="There are no contacts to edit." />
      </Screen>
    );
  }
  return (
    <Screen nav={nav}>
      <Heading id="heading.edit" text={`Edit ${contact.name}`} />
      <Form
        id="form.edit"
        onSubmit={(values) => {
          contacts.update(contact.id, { name: values["name"] ?? contact.name, email: values["email"] ?? contact.email });
          nav_.go("screen.contact", { contact: contact.id });
        }}
        actions={<Button id="btn.update" label="Save changes" emphasis="primary" submit />}
      >
        <Field id="field.edit-name" name="name" label="Name" defaultValue={contact.name} required />
        <Field id="field.edit-email" name="email" label="Email" defaultValue={contact.email} required pattern={EMAIL} patternMessage="Enter an email address" />
      </Form>
    </Screen>
  );
}

function Settings() {
  const role = useRole();
  const nav_ = useNav();
  const [company, setCompany] = useValue<string>("company");
  return (
    <Screen nav={nav}>
      <Heading id="heading.settings" text="Settings" />
      <Form
        id="form.settings"
        onSubmit={(values) => {
          setCompany(values["company"] ?? company);
          nav_.go("screen.contacts");
        }}
        actions={role === "editor" ? <Button id="btn.save-settings" label="Save settings" emphasis="primary" submit /> : undefined}
      >
        <Field id="field.company" name="company" label="Company" defaultValue={company} required />
        <Field id="field.confirm" name="confirm" label="Confirm company change" type="switch" defaultValue="off" required />
      </Form>
    </Screen>
  );
}

export default defineApp({
  screens: {
    "screen.contacts": ContactList,
    "screen.contact": ContactDetail,
    "screen.new": NewContact,
    "screen.edit": EditContact,
    "screen.settings": Settings,
  },
  data: {
    company: "Acme",
    contacts: [
      { id: "contacts-1", name: "Ada Lovelace", email: "ada@example.com", added: "2025-11-02" },
      { id: "contacts-2", name: "Alan Turing", email: "alan@example.com", added: "2025-12-10" },
      { id: "contacts-3", name: "Katherine Johnson", email: "katherine@example.com", added: "2026-01-05" },
    ],
  },
});
