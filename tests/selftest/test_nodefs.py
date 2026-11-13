"""lib/nodefs.py: a node's files through its root, on a real temp dir for a
host path, and against a faked docker CLI for a Docker root.

why faked: the selftests need no engine; a live Docker world is checked on a
host that runs one.
"""
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from lib import nodefs
from lib.worlds import node_entry

C = "astral-20261104t000000z-w0-node1"


class TestLocalRoot(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        # why a symlinked root: a local world's root lies under the session
        # tree's symlink to the temp dir
        self.real = Path(tmp.name).resolve() / "real"
        self.real.mkdir()
        (Path(tmp.name) / "session").symlink_to(self.real)
        self.node = node_entry(root=str(Path(tmp.name) / "session"))

    def test_the_path_is_resolved_as_the_node_stores_it(self):
        self.assertEqual(nodefs.path(self.node, "w/a.txt"),
                         str(self.real / "w" / "a.txt"))
        self.assertEqual(nodefs.path(self.node), str(self.real))

    def test_write_creates_parents_and_reads_back(self):
        nodefs.write_bytes(self.node, "w/sub/a.txt", b"\x00\xffhi")
        self.assertEqual(nodefs.read_bytes(self.node, "w/sub/a.txt"),
                         b"\x00\xffhi")
        self.assertEqual(nodefs.read_text(self.node, "w/sub/a.txt"),
                         "\x00�hi")

    def test_listdir_names_each_kind_and_never_follows_a_link(self):
        nodefs.mkdir(self.node, "w/dir")
        nodefs.mkdir(self.node, "w/dir")
        nodefs.write_bytes(self.node, "w/f.txt", b"f")
        nodefs.symlink(self.node, "w/link", "w/dir")
        self.assertEqual(nodefs.listdir(self.node, "w"),
                         {"dir": "dir", "f.txt": "file", "link": "link"})
        self.assertEqual(os.readlink(self.real / "w" / "link"),
                         str(self.real / "w" / "dir"))

    def test_remove_takes_a_file_a_link_or_a_tree(self):
        nodefs.write_bytes(self.node, "w/sub/a.txt", b"a")
        nodefs.write_bytes(self.node, "w/b.txt", b"b")
        nodefs.symlink(self.node, "w/link", "w/sub")
        nodefs.remove(self.node, "w/link")
        self.assertTrue((self.real / "w" / "sub" / "a.txt").is_file())
        nodefs.remove(self.node, "w/b.txt")
        nodefs.remove(self.node, "w/sub")
        self.assertEqual(nodefs.listdir(self.node, "w"), {})

    def test_rename_replaces_the_target(self):
        nodefs.write_bytes(self.node, "a", b"new")
        nodefs.write_bytes(self.node, "b", b"old")
        nodefs.rename(self.node, "a", "b")
        self.assertEqual(nodefs.listdir(self.node), {"b": "file"})
        self.assertEqual(nodefs.read_bytes(self.node, "b"), b"new")

    def test_set_mtime(self):
        nodefs.write_bytes(self.node, "a", b"a")
        nodefs.set_mtime(self.node, "a", 1_700_000_000)
        self.assertEqual((self.real / "a").stat().st_mtime, 1_700_000_000)

    def test_log_is_astrald_log_under_the_root(self):
        (self.real / "astrald.log").write_bytes(b"started\n")
        self.assertEqual(nodefs.log(self.node), "started\n")


class TestDockerRoot(unittest.TestCase):
    def setUp(self):
        self.node = node_entry(root=f"docker:{C}:/var/lib/astrald")
        self.calls = []
        self.out = b""
        p = mock.patch.object(nodefs.subprocess, "run", self.docker)
        p.start()
        self.addCleanup(p.stop)

    def docker(self, cmd, input=None, capture_output=False, stdin=None):
        self.calls.append((cmd, input, stdin))
        return subprocess.CompletedProcess(cmd, 0, self.out, b"")

    def argv(self):
        return [c[0] for c in self.calls]

    def test_the_path_is_the_containers(self):
        self.assertEqual(nodefs.path(self.node, "w/a.txt"),
                         "/var/lib/astrald/w/a.txt")
        self.assertEqual(self.calls, [])

    def test_a_read_cats_the_file_with_no_stdin(self):
        self.out = b"\x00\xff"
        self.assertEqual(nodefs.read_bytes(self.node, "w/a"), b"\x00\xff")
        self.assertEqual(self.calls, [(
            ["docker", "exec", C, "cat", "--", "/var/lib/astrald/w/a"],
            None, subprocess.DEVNULL)])

    def test_the_log_is_astrald_log_under_the_root(self):
        self.out = b"started\n"
        self.assertEqual(nodefs.log(self.node), "started\n")
        self.assertEqual(self.argv(), [["docker", "exec", C, "cat", "--",
                                        "/var/lib/astrald/astrald.log"]])

    def test_a_write_streams_its_bytes_in_and_creates_parents(self):
        nodefs.write_bytes(self.node, "w/sub/a", b"data")
        self.assertEqual(self.calls, [(
            ["docker", "exec", "-i", C, "sh", "-c",
             'mkdir -p -- "$(dirname -- "$1")" && cat > "$1"', "sh",
             "/var/lib/astrald/w/sub/a"], b"data", None)])

    def test_each_operation_runs_its_command_in_the_container(self):
        self.out = b"f a.txt\0d sub\0l link\0p fifo\0"
        self.assertEqual(nodefs.listdir(self.node, "w"),
                         {"a.txt": "file", "sub": "dir", "link": "link",
                          "fifo": "other"})
        nodefs.mkdir(self.node, "w/d")
        nodefs.remove(self.node, "w/d")
        nodefs.symlink(self.node, "w/link", "out/g")
        nodefs.rename(self.node, "w/a", "w/b")
        nodefs.set_mtime(self.node, "w/b", 1700000000)
        r = "/var/lib/astrald"
        self.assertEqual(self.argv(), [
            ["docker", "exec", C, "find", f"{r}/w", "-mindepth", "1",
             "-maxdepth", "1", "-printf", "%y %f\\0"],
            ["docker", "exec", C, "mkdir", "-p", "--", f"{r}/w/d"],
            ["docker", "exec", C, "rm", "-rf", "--", f"{r}/w/d"],
            ["docker", "exec", C, "ln", "-s", "--", f"{r}/out/g",
             f"{r}/w/link"],
            ["docker", "exec", C, "mv", "-f", "--", f"{r}/w/a", f"{r}/w/b"],
            ["docker", "exec", C, "touch", "-m", "-a", "-d", "@1700000000",
             "--", f"{r}/w/b"],
        ])

    def test_a_failing_command_is_an_os_error_naming_it(self):
        def fails(cmd, **_):
            return subprocess.CompletedProcess(cmd, 1, b"",
                                               b"cat: x: No such file")
        with mock.patch.object(nodefs.subprocess, "run", fails), \
                self.assertRaisesRegex(OSError, "exit 1: cat: x: No such"):
            nodefs.read_bytes(self.node, "x")


class TestOtherRoots(unittest.TestCase):
    def test_a_root_nodefs_cannot_reach_is_refused(self):
        for root in (None, "netsim:sim1:node1", "relative/dir", "docker:c:",
                     "docker::/var/lib/astrald"):
            with self.subTest(root=root), \
                    self.assertRaisesRegex(nodefs.NodeFSError,
                                           "neither a host path"):
                nodefs.read_bytes(node_entry(root=root), "astrald.log")


if __name__ == "__main__":
    unittest.main()
