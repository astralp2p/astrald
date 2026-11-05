"""Run one driver or oracle script; exit FAULT when its own code broke.

    python3 lib/step.py <script.py>

An oracle reports astrald misbehaving by failing an assertion, or by an error
astral-py raises for what the node answered. A TypeError, NameError or
KeyError is neither: it is the script's own code meeting a value it was not
written for, and a verdict of "astrald misbehaved" would send the reader to
the wrong layer. The runner reads FAULT at the verify step as failure kind
`oracle`; at the driver step any non-zero exit is the driver's.

note: astral-py decoding a malformed answer could in principle raise one of
FAULTS, which then reads as `oracle`; the traceback in verify.log is what
tells the two apart.
"""
import runpy
import sys
import traceback
from pathlib import Path

FAULT = 3
FAULTS = (TypeError, NameError, AttributeError, KeyError, IndexError,
          ImportError, SyntaxError)


def main(script: str) -> int:
    path = Path(script).resolve()
    # why: run as `python3 <script>` would be — its own directory first on
    # sys.path, not lib/, whose module names would shadow the stdlib's
    sys.path[0] = str(path.parent)
    sys.argv = [str(path)]
    try:
        runpy.run_path(str(path), run_name="__main__")
    except FAULTS:
        # why: stdout and stderr share one log, and the script's buffered
        # prints would land after the traceback, reading like its last word
        sys.stdout.flush()
        traceback.print_exc()
        print(f"[step] {path.name} raised a programming error, not a verdict",
              file=sys.stderr)
        return FAULT
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1]))
