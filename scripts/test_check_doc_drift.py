import unittest
from check_doc_drift import compare


class DocDriftTests(unittest.TestCase):
    def test_unchanged(self):
        compare({"source": {"sha256": "a"}}, {"source": {"sha256": "a"}})

    def test_changes_and_missing_sources_are_not_passes(self):
        for reviewed, current in (({}, {}), ({"a": 1}, {"b": 1}), ({"a": 1}, {"a": 2})):
            with self.subTest(reviewed=reviewed, current=current), self.assertRaises(ValueError):
                compare(reviewed, current)


if __name__ == "__main__":
    unittest.main()
