import os
import tempfile
import unittest

import envfile


class ParseTest(unittest.TestCase):
    def test_basic_lines_comments_and_export(self):
        text = "A=1\nB = two \n\n# comment\n   # indented comment\nexport C=3\n"
        self.assertEqual(envfile.parse(text), {"A": "1", "B": "two", "C": "3"})

    def test_value_containing_equals(self):
        text = "DATABASE_URL=postgres://app:secret@db:5432/app?sslmode=require&x=y\n"
        self.assertEqual(
            envfile.parse(text),
            {"DATABASE_URL": "postgres://app:secret@db:5432/app?sslmode=require&x=y"},
        )

    def test_hash_in_unquoted_values(self):
        text = "PORT=8080 # web port\nTHEME_COLOR=#1d4ed8\nDOCS=https://example.com/guide#setup\nTABBED=on\t# comment\n"
        self.assertEqual(
            envfile.parse(text),
            {
                "PORT": "8080",
                "THEME_COLOR": "#1d4ed8",
                "DOCS": "https://example.com/guide#setup",
                "TABBED": "on",
            },
        )

    def test_single_quotes_are_literal(self):
        text = r"PASSWORD='p#ss w=rd \n \"x\"'  # rotated monthly" + "\n"
        self.assertEqual(envfile.parse(text), {"PASSWORD": r"p#ss w=rd \n \"x\""})

    def test_double_quotes_support_escapes(self):
        text = r'GREETING="Hello\n\"friend\"\t\\ # not a comment"   # a comment' + "\n"
        self.assertEqual(envfile.parse(text), {"GREETING": 'Hello\n"friend"\t\\ # not a comment'})

    def test_unknown_escape_is_kept(self):
        self.assertEqual(envfile.parse(r'PATTERN="a\d+"'), {"PATTERN": r"a\d+"})

    def test_empty_values(self):
        self.assertEqual(
            envfile.parse("EMPTY=\nEMPTY_QUOTED=\"\"\nEMPTY_SINGLE=''\n"),
            {"EMPTY": "", "EMPTY_QUOTED": "", "EMPTY_SINGLE": ""},
        )

    def test_last_value_wins(self):
        self.assertEqual(envfile.parse("MODE=dev\nMODE=prod\n"), {"MODE": "prod"})

    def test_errors_report_line_number(self):
        cases = {
            "OK=1\nJUST_A_WORD\n": 2,
            "1BAD=x\n": 1,
            "A=1\n\nOPEN=\"abc\n": 3,
            "OPEN='abc\n": 1,
            'X="abc" junk\n': 1,
            "BAD-KEY=1\n": 1,
        }
        for text, lineno in cases.items():
            with self.subTest(text=text):
                with self.assertRaises(envfile.ParseError) as ctx:
                    envfile.parse(text)
                self.assertEqual(ctx.exception.lineno, lineno)
                self.assertTrue(str(ctx.exception).startswith(f"line {lineno}:"))


class LoadTest(unittest.TestCase):
    def setUp(self):
        fd, self.path = tempfile.mkstemp(suffix=".env")
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            f.write('APP_ENV=test\nSECRET="s#cret"\n')
        self.addCleanup(os.remove, self.path)

    def test_load_keeps_existing_values(self):
        environ = {"APP_ENV": "production"}
        values = envfile.load(self.path, environ=environ)
        self.assertEqual(values, {"APP_ENV": "test", "SECRET": "s#cret"})
        self.assertEqual(environ, {"APP_ENV": "production", "SECRET": "s#cret"})

    def test_load_override(self):
        environ = {"APP_ENV": "production"}
        envfile.load(self.path, environ=environ, override=True)
        self.assertEqual(environ, {"APP_ENV": "test", "SECRET": "s#cret"})


if __name__ == "__main__":
    unittest.main()
