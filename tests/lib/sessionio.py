"""What drivers and oracles are allowed to see: session.json in, facts out."""
import json
import os
from pathlib import Path


def load() -> dict:
    return json.loads(Path(os.environ["ASTRAL_TESTS_SESSION"]).read_text())


def write_facts(d: dict) -> None:
    out = os.environ.get("ASTRAL_TESTS_FACTS_OUT")
    if out:
        Path(out).write_text(json.dumps(d, indent=2) + "\n")


def as_bytes(obj) -> bytes:
    """The bytes an object carries, whatever shape the client handed back.

    why: `bytes(obj)` raises TypeError on a str — "string argument without an
    encoding" — which tells a reader nothing about whether the content
    matched. A scripted driver stores a raw blob and gets bytes back; an
    operator told to "store the contents of ~/payload.txt" may reasonably
    store it as text and get a str back. Both hold the same characters, and an
    oracle comparing content should say so rather than dying on the wrapper.
    """
    if isinstance(obj, bytes):
        return obj
    if isinstance(obj, str):
        return obj.encode()
    if isinstance(obj, (bytearray, memoryview)):
        return bytes(obj)
    try:
        return bytes(obj)
    except TypeError:
        return str(obj).encode()
