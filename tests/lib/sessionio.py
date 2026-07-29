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
