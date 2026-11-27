"""Tests of the parts of pack_check.py that need no control plane.

Run: python3 -m unittest discover -s scripts/verify -p 'test_*.py'
"""

import tempfile
import unittest
from pathlib import Path

import pack_check

REPO = Path(__file__).resolve().parents[2]
EXAMPLE = REPO / "apps" / "paperboat"


class LoadPackTest(unittest.TestCase):
    """The manifest gives the names that the checks use."""

    def test_example_pack(self) -> None:
        """The example pack names its first release, version variable and layer."""
        pack = pack_check.load_pack(EXAMPLE)
        self.assertEqual(pack.name, "paperboat")
        self.assertEqual(pack.first, "1.0.0")
        self.assertEqual(pack.variable, "paperboat_version")
        self.assertEqual(pack.tfvars, "platform/stacks/local/50-app/terraform.tfvars")

    def test_missing_manifest(self) -> None:
        """A directory without pack.json is refused with its path."""
        with tempfile.TemporaryDirectory() as empty, self.assertRaises(pack_check.CheckError):
            pack_check.load_pack(Path(empty))

    def test_manifest_without_releases(self) -> None:
        """A manifest that lacks a value is refused, not read as empty."""
        with tempfile.TemporaryDirectory() as directory:
            (Path(directory) / "pack.json").write_text('{"name": "x"}', encoding="utf-8")
            with self.assertRaises(pack_check.CheckError):
                pack_check.load_pack(Path(directory))


class SetVersionTest(unittest.TestCase):
    """The version line changes and nothing else does."""

    def test_tfvars_of_the_example_pack(self) -> None:
        """The local tfvars of the example pack gets the new version in its aligned line."""
        pack = pack_check.load_pack(EXAMPLE)
        text = (REPO / pack.tfvars).read_text(encoding="utf-8")
        changed, previous = pack_check.set_version(text, pack.variable, "1.1.0")
        self.assertEqual(previous, "1.0.0")
        self.assertIn('paperboat_version = "1.1.0"\n', changed)
        self.assertEqual(changed.replace('"1.1.0"', '"1.0.0"'), text)

    def test_longer_name_with_the_same_start_is_not_the_line(self) -> None:
        """A variable whose name only starts with the version variable stays as it is."""
        text = 'app_version_note = "x"\napp_version      = "2.0.0"\n'
        changed, previous = pack_check.set_version(text, "app_version", "2.1.0")
        self.assertEqual(previous, "2.0.0")
        self.assertEqual(changed, 'app_version_note = "x"\napp_version      = "2.1.0"\n')

    def test_no_line(self) -> None:
        """A file that does not set the variable is refused."""
        with self.assertRaises(pack_check.CheckError):
            pack_check.set_version('other = "1"\n# app_version = "1"\n', "app_version", "2")

    def test_two_lines(self) -> None:
        """A file that sets the variable twice is refused."""
        with self.assertRaises(pack_check.CheckError):
            pack_check.set_version('v = "1"\nv = "2"\n', "v", "3")


class CopyRecordTest(unittest.TestCase):
    """A bad version of a pack with records gets the record of the version before it."""

    RECORD = b'{"values": {"image": {"tag": "2.0.0"}}, "schema_change": true}\n'

    def clone_with(self, files: dict[str, bytes]) -> Path:
        """Return a directory, removed after the test, that holds files at their paths."""
        work = tempfile.TemporaryDirectory()
        self.addCleanup(work.cleanup)
        clone = Path(work.name)
        for name, content in files.items():
            path = clone / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(content)
        return clone

    def test_copy_of_the_previous_record(self) -> None:
        """The new record has the bytes of the previous one, schema_change included."""
        clone = self.clone_with({"apps/kc/releases/2.0.0.json": self.RECORD})
        record = pack_check.copy_record(clone, "kc", "2.0.0", "2.0.1")
        self.assertEqual(record, clone / "apps/kc/releases/2.0.1.json")
        self.assertEqual(record.read_bytes(), self.RECORD)

    def test_pack_without_records(self) -> None:
        """A pack with no record of the previous version gets no record."""
        clone = self.clone_with({"apps/kc/chart/Chart.yaml": b"name: kc\n"})
        self.assertIsNone(pack_check.copy_record(clone, "kc", "2.0.0", "2.0.1"))
        self.assertFalse((clone / "apps/kc/releases/2.0.1.json").exists())

    def test_record_of_another_pack(self) -> None:
        """Only the records of the pack itself count."""
        clone = self.clone_with({"apps/other/releases/2.0.0.json": self.RECORD})
        self.assertIsNone(pack_check.copy_record(clone, "kc", "2.0.0", "2.0.1"))

    def test_existing_record_is_kept(self) -> None:
        """A version with a record is a real release: the check stops and changes nothing."""
        real = b'{"schema_change": false}\n'
        clone = self.clone_with(
            {"apps/kc/releases/2.0.0.json": self.RECORD, "apps/kc/releases/2.0.1.json": real}
        )
        with self.assertRaises(pack_check.CheckError):
            pack_check.copy_record(clone, "kc", "2.0.0", "2.0.1")
        self.assertEqual((clone / "apps/kc/releases/2.0.1.json").read_bytes(), real)


class RunningVersionsTest(unittest.TestCase):
    """Only a passed verification reports a running version."""

    def test_passed_and_failed(self) -> None:
        """The checks of verify.passed count; verify.failed and other lines do not."""
        events = [
            {"type": "verify.failed", "data": {"checks": ["running version is 1.2.0"]}},
            {"type": "verify.passed", "data": {"checks": ["check ready answers 200"]}},
            {"type": "verify.passed", "data": {"checks": ["running version is 1.1.0"]}},
            {"type": "verify.passed", "data": None},
            {"type": "job.finished", "data": {"checks": ["running version is 9.9.9"]}},
        ]
        self.assertEqual(pack_check.running_versions(events), ["1.1.0"])

    def test_no_event(self) -> None:
        """No events report no version."""
        self.assertEqual(pack_check.running_versions([]), [])


class RestartComponentsTest(unittest.TestCase):
    """The definition of the restart runbook names the components."""

    @staticmethod
    def definition(**changes: object) -> dict[str, object]:
        """Return a definition of the restart runbook with the given changes."""
        base: dict[str, object] = {
            "name": "restart-component",
            "standing_approver": "carol",
            "inputs": {"component": {"type": "string", "values": ["api", "web"]}},
        }
        return base | changes

    def test_permitted_values(self) -> None:
        """The values of the input are the components, in their order."""
        runbooks = [{"name": "db-backup"}, self.definition()]
        self.assertEqual(pack_check.restart_components(runbooks), ["api", "web"])

    def test_refusals(self) -> None:
        """A missing runbook, approval or value stops the check with a reason."""
        cases = {
            "no runbook": [{"name": "db-backup"}],
            "no standing approval": [self.definition(standing_approver="")],
            "no value": [self.definition(inputs={"component": {"type": "string"}})],
            "no input": [self.definition(inputs=None)],
        }
        for name, runbooks in cases.items():
            with self.subTest(name), self.assertRaises(pack_check.CheckError):
                pack_check.restart_components(runbooks)


if __name__ == "__main__":
    unittest.main()
