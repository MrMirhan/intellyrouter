# tailn

`tailn` prints the last lines of one or more files. We ship it in our minimal
container images, where coreutils `tail` is not available.

```
usage: tailn [-n lines] [-q] [--] [file ...]
```

| flag                           | meaning                                   |
|--------------------------------|-------------------------------------------|
| `-n N`, `-n=N`, `--lines N`, `--lines=N` | print the last N lines (default 10) |
| `-q`, `--quiet`                | do not print `==> name <==` headers       |
| `--`                           | stop flag parsing; later arguments are files |

With no file arguments, or with the file name `-`, `tailn` reads standard input.

Exit status:

- `0`: success
- `1`: at least one file could not be read (the other files are still printed)
- `2`: invalid command line; `tailn` prints the error and the usage line to stderr

Build with `go build -o tailn .`.
