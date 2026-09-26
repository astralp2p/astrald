"""The external authority a driver serves on a node's `authority_url`.

A node asks it about the three actions `nodeconfig.AUTH_YAML` names:
`mod.messaging.send_action` of a sender before a message leaves,
`mod.messaging.receive_action` of a recipient before a delivery is stored, and
`mod.messaging.read_mailbox_action` of a reader before it reads a mailbox that
is not its own. No handler grants any of the three, so what a node carries and
whom it lets read is what this authority admits.
"""
import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit

SEND = "mod.messaging.send_action"
RECEIVE = "mod.messaging.receive_action"
READ = "mod.messaging.read_mailbox_action"


class Authority:
    """Allows a question only when (action type, actor, other party) is in
    `allowed`, and records every question with the answer it gave.

    The other party is ToID for a send, FromID for a receive, and MailboxID
    for a read.
    """

    def __init__(self, url: str, allowed: set):
        self.allowed = allowed
        self.questions = []
        authority = self

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
                allow = authority.decide(json.loads(body))
                answer = json.dumps({"allow": allow}).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(answer)))
                self.end_headers()
                self.wfile.write(answer)

            def log_message(self, *args):
                pass

        where = urlsplit(url)
        self.server = ThreadingHTTPServer((where.hostname, where.port), Handler)
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def decide(self, envelope: dict) -> bool:
        kind = envelope.get("Type")
        obj = envelope.get("Object") or {}
        actor = ((obj.get("Action") or {}).get("ActorID") or "").lower()
        other = (obj.get("ToID") or obj.get("FromID")
                 or obj.get("MailboxID") or "").lower()
        allow = (kind, actor, other) in self.allowed
        self.questions.append(
            {"type": kind, "actor": actor, "other": other, "allow": allow})
        return allow

    def close(self):
        self.server.shutdown()
        self.server.server_close()


def answers(questions: list, kind: str, actor: str, other: str) -> list:
    """The answers a record holds for one (action, actor, other party)."""
    return [q["allow"] for q in questions
            if (q["type"], q["actor"], q["other"]) == (kind, actor, other)]
