"""A minimal MCP client: what an agent is, as far as astrald can tell.

why hand-rolled: the harness is stdlib-only by design, and reaching the MCP
server takes three JSON-RPC calls over HTTP. An SDK would be a dependency the
test tree does not have, for `initialize`, `notifications/initialized` and
`tools/call`.

why it matters that this is the real door: a driver that called the tools in
process would prove nothing about what a bearer token reaches. The guard under
test reads a key set on the way through this exact path.
"""
import json
import urllib.error
import urllib.request

PROTOCOL_VERSION = "2025-06-18"


class ToolError(Exception):
    """The server ran the tool and the tool refused, or the call itself failed.

    Carries the text so an oracle can tell a refusal from a timeout — both
    leave the caller with no result, for entirely different reasons.
    """


class MCPClient:
    """One MCP session, authenticated as one agent by its personal access token."""

    def __init__(self, url: str, token: str, timeout: float = 30.0):
        self.url = url.rstrip("/") + "/"
        self.token = token
        self.timeout = timeout
        self.session_id = None
        self._next_id = 0

    def __enter__(self):
        self._initialize()
        return self

    def __exit__(self, *exc):
        return False

    # -- protocol ---------------------------------------------------------

    def _initialize(self) -> None:
        self._rpc("initialize", {
            "protocolVersion": PROTOCOL_VERSION,
            "capabilities": {},
            "clientInfo": {"name": "astral-tests", "version": "0"},
        })
        self._notify("notifications/initialized")

    def call_tool(self, name: str, arguments: dict) -> dict:
        """Run a tool. Raises ToolError when the server reports one."""
        result = self._rpc("tools/call", {"name": name, "arguments": arguments})

        # why check isError as well as the JSON-RPC error: a tool that returns
        # an error reports it inside a successful response, so a client reading
        # only the envelope sees every refusal as a success.
        if result.get("isError"):
            raise ToolError(_text_of(result))

        return result

    def list_tools(self) -> list:
        return self._rpc("tools/list", {}).get("tools", [])

    # -- transport --------------------------------------------------------

    def _rpc(self, method: str, params: dict) -> dict:
        self._next_id += 1
        body, headers = self._post({
            "jsonrpc": "2.0", "id": self._next_id,
            "method": method, "params": params,
        })

        sid = headers.get("Mcp-Session-Id")
        if sid:
            self.session_id = sid

        message = _decode(body)
        if "error" in message:
            raise ToolError(json.dumps(message["error"]))
        return message.get("result", {})

    def _notify(self, method: str, params: dict | None = None) -> None:
        self._post({"jsonrpc": "2.0", "method": method,
                    "params": params or {}})

    def _post(self, payload: dict) -> tuple[bytes, dict]:
        req = urllib.request.Request(
            self.url, data=json.dumps(payload).encode(), method="POST")
        req.add_header("Content-Type", "application/json")
        req.add_header("Accept", "application/json, text/event-stream")
        req.add_header("Authorization", f"Bearer {self.token}")
        req.add_header("MCP-Protocol-Version", PROTOCOL_VERSION)
        if self.session_id:
            req.add_header("Mcp-Session-Id", self.session_id)

        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                return resp.read(), dict(resp.headers)
        except urllib.error.HTTPError as e:
            raise ToolError(f"HTTP {e.code}: {e.read()[:400]!r}") from None


def _decode(body: bytes) -> dict:
    """One JSON-RPC message, whether the server framed it as JSON or as SSE."""
    text = body.decode().strip()
    if not text:
        return {}
    if text.startswith("{"):
        return json.loads(text)

    for line in text.splitlines():
        if line.startswith("data:"):
            return json.loads(line[5:].strip())
    raise ToolError(f"undecodable response: {text[:200]!r}")


def _text_of(result: dict) -> str:
    """Whatever the tool said, flattened — the oracle matches on this."""
    parts = [c.get("text", "") for c in result.get("content", [])
             if isinstance(c, dict)]
    return " ".join(p for p in parts if p) or json.dumps(result)
