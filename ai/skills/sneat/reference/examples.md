# Semantic action examples (EN / RU / mixed)

Weekday codes: `mo tu we th fr sa su`. Times: `HH:MM`. Dates: `YYYY-MM-DD`.
Relative: `today | tomorrow | end_of_week | end_of_month | end_of_year`.

## buy

```json
// Buy shoes.
{"kind":"buy","operations":[{"objects":[{"text":"shoes","language":"en"}]}]}

// Купить ботинки.
{"kind":"buy","operations":[{"objects":[{"text":"ботинки","language":"ru","localized":{"en":"shoes"}}]}]}

// Buy shoes for Alice and Vasilisa.
{"kind":"buy","operations":[{"objects":[{"text":"shoes","language":"en"}],
  "contacts":[{"mention":"Alice"},{"mention":"Vasilisa"}]}]}

// Buy shoes and socks for Alice and Vasilisa.  (one group)
{"kind":"buy","operations":[{"objects":[{"text":"shoes","language":"en"},{"text":"socks","language":"en"}],
  "contacts":[{"mention":"Alice"},{"mention":"Vasilisa"}]}]}

// Buy shoes for Alice and a basketball for Vasilisa.  (two groups)
{"kind":"buy","operations":[
  {"objects":[{"text":"shoes","language":"en"}],"contacts":[{"mention":"Alice"}]},
  {"objects":[{"text":"basketball","language":"en"}],"contacts":[{"mention":"Vasilisa"}]}]}

// Купить ботинки и носки для Алисы и Василисы.
{"kind":"buy","operations":[{"objects":[{"text":"ботинки","language":"ru","localized":{"en":"shoes"}},
  {"text":"носки","language":"ru","localized":{"en":"socks"}}],
  "contacts":[{"mention":"Алисы"},{"mention":"Василисы"}]}]}

// Buy Vasilisa basketball shoes by the end of the month.
{"kind":"buy","operations":[{"objects":[{"text":"basketball shoes","language":"en"}],
  "contacts":[{"mention":"Vasilisa"}],"deadline":{"relative":"end_of_month"}}]}

// Купить Василисе баскетбольные кроссовки до пятницы.
{"kind":"buy","operations":[{"objects":[{"text":"баскетбольные кроссовки","language":"ru","localized":{"en":"basketball shoes"}}],
  "contacts":[{"mention":"Василисе"}],"deadline":{"weekday":"fr"}}]}

// Buy 2 litres of milk.
{"kind":"buy","operations":[{"objects":[{"text":"milk","language":"en","quantity":2,"unit":"L"}]}]}

// Buy Vanya a birthday present before his birthday.
{"kind":"buy","operations":[{"objects":[{"text":"birthday present","language":"en"}],"contacts":[{"mention":"Vanya"}],
  "deadline":{"before":{"birthdayOf":{"mention":"Vanya"}}}}]}

// Buy Vasilisa basketball shoes before her next basketball practice.
{"kind":"buy","operations":[{"objects":[{"text":"basketball shoes","language":"en"}],"contacts":[{"mention":"Vasilisa"}],
  "deadline":{"before":{"nextOccurrenceOf":{"activity":{"text":"basketball"},"contact":{"mention":"Vasilisa"}}}}}]}
```

## schedule

```json
// Vasilisa has basketball every Wednesday at 3pm, Friday at 6pm and Saturday at 10am.
{"kind":"schedule","schedule":{"activity":{"text":"basketball","language":"en"},"kind":"activity",
  "contacts":[{"mention":"Vasilisa"}],"repeats":"weekly",
  "slots":[{"weekday":"we","time":"15:00"},{"weekday":"fr","time":"18:00"},{"weekday":"sa","time":"10:00"}]}}

// У Василисы баскетбол каждую среду в 3 часа дня, в пятницу в 6 вечера и в субботу в 10 утра.
{"kind":"schedule","schedule":{"activity":{"text":"баскетбол","language":"ru","localized":{"en":"basketball"}},
  "contacts":[{"mention":"Василисы"}],"repeats":"weekly",
  "slots":[{"weekday":"we","time":"15:00"},{"weekday":"fr","time":"18:00"},{"weekday":"sa","time":"10:00"}]}}

// Vasilisa basketball Friday.   → Sneat asks time + repeats; do NOT guess
{"kind":"schedule","schedule":{"activity":{"text":"basketball","language":"en"},
  "contacts":[{"mention":"Vasilisa"}],"slots":[{"weekday":"fr"}]}}

// (patch) Every Friday at six.
{"schedule":{"slots":[{"weekday":"fr","time":"6","repeats":"weekly"}]}}

// (patch after commit) Actually Saturday is at 11.
{"schedule":{"slots":[{"weekday":"sa","time":"11:00"}]}}

// (patch after commit) This Saturday at 12 only.
{"schedule":{"exceptions":[{"weekday":"sa","time":"12:00"}]}}

// Vasilisa has a dentist appointment Friday at 4:30.
{"kind":"schedule","schedule":{"activity":{"text":"dentist","language":"en"},"kind":"appointment",
  "contacts":[{"mention":"Vasilisa"}],"slots":[{"weekday":"fr","time":"4:30","repeats":"once"}]}}

// У Василисы стоматолог в пятницу в 16:30.
{"kind":"schedule","schedule":{"activity":{"text":"стоматолог","language":"ru","localized":{"en":"dentist"}},"kind":"appointment",
  "contacts":[{"mention":"Василисы"}],"slots":[{"weekday":"fr","time":"16:30","repeats":"once"}]}}

// Dentist Friday.  / В пятницу стоматолог.   → Sneat asks who + time
{"kind":"schedule","schedule":{"activity":{"text":"dentist","language":"en"},"kind":"appointment","slots":[{"weekday":"fr","repeats":"once"}]}}

// Добавь Vasilisa basketball на Friday в 6pm.  (mixed)
{"kind":"schedule","schedule":{"activity":{"text":"basketball","language":"en"},
  "contacts":[{"mention":"Vasilisa"}],"slots":[{"weekday":"fr","time":"18:00"}]}}
```

## birthday

```json
// Vanya's birthday is Friday.
{"kind":"birthday","birthday":{"contact":{"mention":"Vanya"},"date":{"weekday":"fr"}}}

// У Вани в пятницу день рождения.
{"kind":"birthday","birthday":{"contact":{"mention":"Вани"},"date":{"weekday":"fr"}}}

// Vanya was born on 11 September 2015.
{"kind":"birthday","birthday":{"contact":{"mention":"Vanya"},"date":{"date":"2026-09-11"},"year":2015}}
```

## clear

Destructive and never inferred. Confirm with the user before committing.

```json
// Clear the groceries list.  (no listID: Sneat defaults to the shopping list)
{"kind":"clear","operations":[{}]}

// Очисти список продуктов.
{"kind":"clear","operations":[{}]}

// Empty the Toys list.  (listID from `sneat context`)
{"kind":"clear","operations":[{"listID":"to-do!toys"}]}
```

Not a `clear` — these remove one item, which this kind cannot express, and
Sneat refuses a `clear` carrying `objects`:

```text
"take the milk off the list"      "убери молоко из списка"
```

## Answering ambiguity

Validation returned:

```json
{"questions":[{"text":"Which Alice do you mean: Alice Cooper and Alice B?","paths":["operations[0].contacts[0]"],
  "options":[{"id":"c1","label":"Alice Cooper"},{"id":"c2","label":"Alice B"}],"blocking":true}]}
```

Ask the user; then patch with the chosen id:

```json
{"operations":[{"contacts":[{"mention":"Alice","contactID":"c2"}]}]}
```

## Reply patterns

- EN: "Added **shoes for Vasilisa** to Groceries (by Fri 11 Sep). https://sneat.app/space/family/…/list/buy/groceries"
- RU: "Добавил **ботинки для Василисы** в список «Покупки» (до пятницы, 11 сентября). https://sneat.app/…"
- Mixed original: "Добавил «молоко» (milk) в Groceries."
