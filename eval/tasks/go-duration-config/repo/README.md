# duration

Duration values for our YAML and env-based service configs.

`time.ParseDuration` does not know days or weeks, and operators kept writing
`retention: 7d`. This package accepts the same style with two more units:

| unit | meaning      |
|------|--------------|
| `ms` | milliseconds |
| `s`  | seconds      |
| `m`  | minutes      |
| `h`  | hours        |
| `d`  | 24 hours     |
| `w`  | 7 days       |

Values can be combined (`1h30m`, `1d12h`) and can have a fractional part
(`1.5h`, `0.5d`). Every number needs a unit. Negative durations are not allowed
in config files.

```go
ttl, err := duration.Parse(os.Getenv("CACHE_TTL"))
```

`duration.Duration` implements `encoding.TextUnmarshaler`, so it can be used
directly in config structs.
