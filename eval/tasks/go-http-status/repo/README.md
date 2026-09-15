# notes

A small JSON API for notes, used as the backend of our internal scratchpad
tool. Storage is in memory.

## API

| method   | path          | success                         |
|----------|---------------|---------------------------------|
| `GET`    | `/notes`      | `200` with a JSON array, ordered by id |
| `POST`   | `/notes`      | `201`, `Location: /notes/{id}`, the created note |
| `GET`    | `/notes/{id}` | `200` with the note             |
| `DELETE` | `/notes/{id}` | `204` with an empty body        |

A note is `{"id": 1, "title": "Groceries", "body": "milk, eggs"}`.

### Creating a note

- The request must have `Content-Type: application/json`, otherwise `415`.
- The body must be a JSON object and at most 1 MiB. Malformed JSON is `400`;
  a larger body is `413`.
- `title` is required. Leading and trailing spaces are removed. A title that is
  empty after trimming, or longer than 120 characters, is `422`.

### Errors

- An `{id}` that is not a positive integer is `400`.
- An unknown note is `404`.
- A method that the path does not support is `405` with an `Allow` header.

Every error response has a JSON body `{"error": "<message>"}`.

## Running

```go
http.ListenAndServe(":8080", notes.NewHandler(notes.NewStore()))
```
