"""Minimal .env file parser."""

import os
import re

_KEY = re.compile(r"[A-Za-z_][A-Za-z0-9_]*")


class ParseError(ValueError):
    def __init__(self, lineno: int, message: str):
        super().__init__(f"line {lineno}: {message}")
        self.lineno = lineno


def parse(text: str) -> dict[str, str]:
    result: dict[str, str] = {}
    for lineno, raw in enumerate(text.splitlines(), start=1):
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("export "):
            line = line[len("export "):]
        if "=" not in line:
            raise ParseError(lineno, "expected KEY=VALUE")

        key, value = line.split("=")
        key = key.strip()
        if not _KEY.fullmatch(key):
            raise ParseError(lineno, f"invalid key {key!r}")

        value = value.strip()
        if "#" in value:
            value = value[: value.index("#")].strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in "'\"":
            value = value[1:-1]
        result[key] = value
    return result


def load(path: str, environ=None, override: bool = False) -> dict[str, str]:
    """Parse the file at path and copy its values into environ (os.environ by default)."""
    if environ is None:
        environ = os.environ
    with open(path, encoding="utf-8") as f:
        values = parse(f.read())
    for key, value in values.items():
        if override or key not in environ:
            environ[key] = value
    return values
