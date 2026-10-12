import unittest
from check_release import check, python_requirement


class ReleaseMetadataTests(unittest.TestCase):
    def test_development_and_release(self):
        for version, ref in (("0.3.0-dev", "refs/heads/main"), ("0.3.0", "refs/tags/v0.3.0")):
            self.assertEqual(version, check(f'Version = "{version}"', f'<!-- sdk-version: {version} -->', ref))

    def test_missing_mismatched_and_stale_versions(self):
        for source, doc, ref in (("", "", ""), ('Version="bad"', "", ""),
                                 ('Version="0.3.0"', '<!-- sdk-version: 0.2.1 -->', ""),
                                 ('Version="0.3.0"', '<!-- sdk-version: 0.3.0 -->', "refs/tags/v0.2.1")):
            with self.subTest(source=source, ref=ref), self.assertRaises(ValueError):
                check(source, doc, ref)

    def test_requirement_is_pinned_and_not_shell_input(self):
        sha = "e70bf99a89f27102247613bcd5d1aa8923a1cda9"
        self.assertTrue(python_requirement({"python_commit": sha, "python_tag": "v0.7.4"}).endswith("@" + sha))
        for bad in ({}, {"python_commit": "main", "python_tag": "v0.7.4"},
                    {"python_commit": sha, "python_tag": "latest"},
                    {"python_commit": sha + ";evil", "python_tag": "v0.7.4"}):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                python_requirement(bad)


if __name__ == "__main__":
    unittest.main()
