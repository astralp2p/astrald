"""The harness's selftests: milliseconds, no daemon, no VM.

why netsim is fenced off: a VM world boots through the netsim CLI, and a
selftest that forgot to fake it once reached the real one and began baking
a lab. Every netsim the selftests could start — the runner's, a vmop's, an
scp through `netsim ssh` — now resolves to a stub that refuses and exits
non-zero, so a missing fake fails its test instead of touching the host.
"""
import atexit
import os
import shutil
import stat
import tempfile
from pathlib import Path

_FENCE = Path(tempfile.mkdtemp(prefix="astral-selftest-fence-"))
atexit.register(shutil.rmtree, _FENCE, True)
(_FENCE / "netsim").write_text(
    "#!/bin/sh\necho 'selftest: netsim is fenced off' >&2\nexit 97\n")
(_FENCE / "netsim").chmod(stat.S_IRWXU)
os.environ["PATH"] = f"{_FENCE}{os.pathsep}{os.environ.get('PATH', '')}"
