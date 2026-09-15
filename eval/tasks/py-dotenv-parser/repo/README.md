# envfile

Loads `.env` files for local development and our deploy scripts, without
pulling in a dependency.

```python
import envfile

envfile.load(".env")                  # keeps variables that are already set
envfile.load(".env.test", override=True)
values = envfile.parse(open(".env").read())
```

## Syntax

```sh
# Comments start with '#'.
export APP_ENV=production        # 'export ' is optional
PORT = 8080                      # spaces around the key and value are ignored
DATABASE_URL=postgres://app:secret@db:5432/app?sslmode=require
THEME_COLOR=#1d4ed8              # '#' starts a comment only after whitespace
PASSWORD='literal #, $ and \n'   # single quotes: everything is literal
GREETING="Hello\n\"friend\""     # double quotes: \n \t \" \\ are escapes
EMPTY=
```

- Only the first `=` separates the key from the value.
- Keys must match `[A-Za-z_][A-Za-z0-9_]*`.
- After a closing quote, only whitespace or a comment may follow.
- If a key appears more than once, the last value wins.

Invalid lines raise `envfile.ParseError`, which has a `lineno` attribute and a
message that starts with `line N:`.
