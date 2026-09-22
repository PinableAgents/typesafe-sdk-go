import unittest
from check_coverage import parse_profile, enforce


class CoverageGateTests(unittest.TestCase):
    def test_merge_independent_package_counters(self):
        blocks = parse_profile("mode: atomic\np/a.go:1.1,2.2 2 0\np/a.go:1.1,2.2 2 4\n")
        self.assertEqual(enforce(blocks, {"p/a.go:1.1,2.2": 2})["statements"], 2)

    def test_fail_closed(self):
        for text in ("", "mode: bad", "mode: atomic", "mode: count\np/a.go:1.1,2.2 0 0",
                     "mode: count\ninvalid", "mode: count\np/a.go:1.1,2.2 1 -1",
                     "mode: count\np/a.go:1.1,2.2 -1 1",
                     "mode: count\np/a.go:1.1,2.2 1 1\np/a.go:1.1,2.2 2 1"):
            with self.subTest(text=text), self.assertRaises(ValueError):
                parse_profile(text)

    def test_no_rounding_loophole(self):
        with self.assertRaises(ValueError):
            enforce(parse_profile("mode: count\np/a.go:1.1,2.2 100000 1\np/a.go:3.1,4.2 1 0"))

    def test_missing_extra_or_changed_source_denominator(self):
        blocks = parse_profile("mode: set\np/a.go:1.1,2.2 1 1\n\n")
        for expected in ({}, {"p/a.go:1.1,2.2": 2}, {"p/b.go:1.1,2.2": 1}):
            with self.subTest(expected=expected), self.assertRaises(ValueError):
                enforce(blocks, expected)
        with self.assertRaises(ValueError):
            enforce({})


if __name__ == "__main__":
    unittest.main()
