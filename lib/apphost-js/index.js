// JavaScript client for the astrald apphost WebSocket protocol.
//
// Speaks `astral.json.v1` against `/.ws` on the apphost HTTP endpoint. Works in
// modern browsers and Node 21+ (uses the native WebSocket global). For older Node,
// install the `ws` package and set `globalThis.WebSocket = require('ws')` before
// calling `connect()`.
//
// See ../../mod/apphost/protocol.md for the underlying protocol.

/** @typedef {{type: string, value: any}} AstralObject — friendly shape used throughout the API. */
/** @typedef {{Type: string, Object: any}} WireEnvelope — the on-the-wire JSONAdapter shape. */

const SUBPROTOCOL = 'astral.json.v1';

const T = {
  HostInfo:        'mod.apphost.host_info_msg',
  AuthToken:       'mod.apphost.auth_token_msg',
  AuthSuccess:     'mod.apphost.auth_success_msg',
  Error:           'mod.apphost.error_msg',
  RouteQuery:      'mod.apphost.route_query_msg',
  QueryAccepted:   'mod.apphost.query_accepted_msg',
  QueryRejected:   'mod.apphost.query_rejected_msg',
  RegisterService: 'mod.apphost.register_service_msg',
  IncomingQuery:   'mod.apphost.incoming_query_msg',
  RejectIncoming:  'mod.apphost.reject_incoming_msg',
  AttachQuery:     'mod.apphost.attach_query_msg',
  Ack:             'ack',
  EOS:             'eos',
};

// ---------- errors ----------

export class ConnectError  extends Error { constructor(m, cause) { super(m); this.name = 'ConnectError';  this.cause = cause; } }
export class AuthError     extends Error { constructor(code)     { super('auth failed: ' + code); this.name = 'AuthError';     this.code = code; } }
export class QueryRejected extends Error { constructor(code)     { super('query rejected (code ' + code + ')'); this.name = 'QueryRejected'; this.code = code; } }
export class RouteNotFound extends Error { constructor()         { super('route not found'); this.name = 'RouteNotFound'; } }
export class ProtocolError extends Error { constructor(m)        { super(m); this.name = 'ProtocolError'; } }

export const errors = { ConnectError, AuthError, QueryRejected, RouteNotFound, ProtocolError };

// ---------- wire helpers ----------

/** Generate a 16-hex-character Nonce string (matches astral.Nonce JSON form). */
function newNonce() {
  const b = crypto.getRandomValues(new Uint8Array(8));
  let s = '';
  for (const x of b) s += x.toString(16).padStart(2, '0');
  return s;
}

/** @param {AstralObject} obj  @returns {WireEnvelope} */
function wrap({ type, value }) {
  return { Type: type, Object: value === undefined ? null : value };
}

/** @param {WireEnvelope} env  @returns {AstralObject} */
function unwrap(env) {
  return { type: env.Type, value: env.Object };
}

function rawSend(ws, env) { ws.send(JSON.stringify(env)); }

// ---------- session bring-up ----------

/**
 * Opens a WS, performs the handshake (HostInfoMsg + optional AuthTokenMsg). Internal helper.
 * @param {string} url
 * @param {string|null} token
 * @param {{skipAuth?: boolean}} [opts]
 * @returns {Promise<{ws: WebSocket, hostInfo: {identity: string, alias: string}, guestID: string|null, recv: Receiver}>}
 */
async function openSession(url, token, opts = {}) {
  const { skipAuth = false } = opts;
  const ws = new WebSocket(url, SUBPROTOCOL);
  const recv = new Receiver(ws);

  await new Promise((resolve, reject) => {
    ws.addEventListener('open', () => resolve(), { once: true });
    ws.addEventListener('error', () => reject(new ConnectError('socket error')), { once: true });
  });

  const hello = await recv.next();
  if (!hello || hello.type !== T.HostInfo) {
    ws.close();
    throw new ProtocolError('expected host_info_msg, got ' + (hello && hello.type));
  }
  const hostInfo = { identity: hello.value.Identity, alias: hello.value.Alias };

  let guestID = null;
  if (token && !skipAuth) {
    rawSend(ws, wrap({ type: T.AuthToken, value: { Token: token } }));
    const resp = await recv.next();
    if (!resp) { ws.close(); throw new ConnectError('socket closed during auth'); }
    if (resp.type === T.Error) { ws.close(); throw new AuthError(resp.value && resp.value.Code); }
    if (resp.type !== T.AuthSuccess) {
      ws.close();
      throw new ProtocolError('expected auth_success_msg, got ' + resp.type);
    }
    guestID = resp.value.GuestID;
  }

  return { ws, hostInfo, guestID, recv };
}

// ---------- frame receiver: turns ws.onmessage into an awaitable queue ----------

class Receiver {
  /** @param {WebSocket} ws */
  constructor(ws) {
    this.ws = ws;
    /** @type {AstralObject[]} */ this.queue = [];
    /** @type {((v: AstralObject|null) => void)[]} */ this.waiters = [];
    this.closed = false;

    ws.addEventListener('message', (ev) => {
      if (typeof ev.data !== 'string') return; // ignore binary frames in JSON mode
      // The server sends one envelope per frame, but tolerate frames carrying
      // several newline-delimited envelopes (e.g. a coalescing relay).
      for (const line of ev.data.split('\n')) {
        if (!line.trim()) continue;
        let env;
        try { env = JSON.parse(line); } catch { continue; }
        const obj = unwrap(env);
        const w = this.waiters.shift();
        if (w) w(obj); else this.queue.push(obj);
      }
    });
    ws.addEventListener('close', () => {
      this.closed = true;
      while (this.waiters.length) this.waiters.shift()(null);
    });
  }

  /** Returns the next AstralObject, or null on close. */
  next() {
    if (this.queue.length) return Promise.resolve(this.queue.shift());
    if (this.closed) return Promise.resolve(null);
    return new Promise((resolve) => this.waiters.push(resolve));
  }
}

// ---------- public API ----------

/**
 * Open a connection to apphost, complete the handshake, and capture host info.
 * Returns a lightweight Host that opens fresh WSes per query / registration.
 *
 * @param {string} url e.g. "ws://127.0.0.1:8624/.ws"
 * @param {{token?: string|null}} [opts]
 * @returns {Promise<Host>}
 */
export async function connect(url, opts = {}) {
  const token = opts.token || null;
  const { ws, hostInfo, guestID } = await openSession(url, token);
  ws.close();
  return new Host(url, token, hostInfo, guestID);
}

export class Host {
  /**
   * @param {string} url
   * @param {string|null} token
   * @param {{identity: string, alias: string}} hostInfo
   * @param {string|null} guestID
   */
  constructor(url, token, hostInfo, guestID) {
    this.url = url;
    this.token = token;
    this.identity = hostInfo.identity;
    this.alias = hostInfo.alias;
    this.guestID = guestID;
  }

  /**
   * Route an outbound query through the host. The returned Stream is async-iterable;
   * each item is an `{type, value}` AstralObject sent by the responder. Iteration ends
   * on `eos` or socket close.
   *
   * @param {string} queryString e.g. "user.info" or "user.info?name=alice"
   * @param {{
   *   target?: string|null,
   *   caller?: string|null,
   *   args?: Record<string, string>,
   *   zone?: string,
   *   filters?: string[]|null,
   * }} [opts]
   * @returns {Promise<Stream>}
   */
  async query(queryString, opts = {}) {
    const { ws, recv } = await openSession(this.url, this.token);

    const qs = opts.args && Object.keys(opts.args).length
      ? queryString + (queryString.includes('?') ? '&' : '?') + new URLSearchParams(opts.args).toString()
      : queryString;

    // Default Caller to the guest identity. Otherwise the host fills it in with
    // its own node identity (core/router.go), which is rarely what callers want.
    const caller = opts.caller !== undefined ? opts.caller : (this.guestID || null);

    rawSend(ws, wrap({
      type: T.RouteQuery,
      value: {
        Nonce:   newNonce(),
        Caller:  caller,
        Target:  opts.target ?? this.identity,
        Query:   qs,
        Zone:    opts.zone ?? 'dvn',
        Filters: opts.filters ?? null,
      },
    }));

    const resp = await recv.next();
    if (!resp) { ws.close(); throw new ConnectError('socket closed before query response'); }
    switch (resp.type) {
      case T.QueryAccepted: return new Stream(ws, recv);
      case T.QueryRejected: ws.close(); throw new QueryRejected(resp.value && resp.value.Code);
      case T.Error:
        ws.close();
        if (resp.value && resp.value.Code === 'route_not_found') throw new RouteNotFound();
        throw new ProtocolError('host error: ' + (resp.value && resp.value.Code));
      default:
        ws.close();
        throw new ProtocolError('unexpected response to route_query: ' + resp.type);
    }
  }

  /**
   * Register this connection as the handler for inbound queries to `identity`.
   * `handler` is called once per inbound query with an IncomingQuery; it must
   * accept (returns a responder Stream) or reject (with a numeric code) within
   * the host's attach timeout (5 s by default), otherwise the caller sees
   * route-not-found.
   *
   * @param {string} identity
   * @param {(q: IncomingQuery) => void|Promise<void>} handler
   * @returns {Promise<Registration>}
   */
  async register(identity, handler) {
    const { ws, recv } = await openSession(this.url, this.token);

    rawSend(ws, wrap({ type: T.RegisterService, value: { Identity: identity } }));
    const ack = await recv.next();
    if (!ack) { ws.close(); throw new ConnectError('socket closed before register ack'); }
    if (ack.type === T.Error) { ws.close(); throw new ProtocolError('register failed: ' + (ack.value && ack.value.Code)); }
    if (ack.type !== T.Ack) {
      ws.close();
      throw new ProtocolError('expected ack, got ' + ack.type);
    }

    return new Registration(this, identity, ws, recv, handler);
  }
}

export class Stream {
  /**
   * @param {WebSocket} ws
   * @param {Receiver} recv
   */
  constructor(ws, recv) {
    this.ws = ws;
    this.recv = recv;
    this.closed = false;
  }

  /** Send an AstralObject over the stream. */
  send(obj) {
    if (this.closed) throw new Error('stream is closed');
    rawSend(this.ws, wrap(obj));
  }

  /** Close the underlying WebSocket. Idempotent. */
  close() {
    if (this.closed) return;
    this.closed = true;
    try { this.ws.close(); } catch { /* ignore */ }
  }

  /** Async iterator. Yields `{type, value}` objects until EOS or close. */
  async *[Symbol.asyncIterator]() {
    while (true) {
      const obj = await this.recv.next();
      if (obj === null) return;
      if (obj.type === T.EOS) return;
      yield obj;
    }
  }
}

export class Registration {
  /**
   * @param {Host} host
   * @param {string} identity
   * @param {WebSocket} ws
   * @param {Receiver} recv
   * @param {(q: IncomingQuery) => void|Promise<void>} handler
   */
  constructor(host, identity, ws, recv, handler) {
    this.host = host;
    this.identity = identity;
    this.ws = ws;
    this.recv = recv;
    this.handler = handler;
    this.closed = false;
    this._loop();
  }

  /** Unregister (closes the registration WS). */
  unregister() {
    if (this.closed) return;
    this.closed = true;
    try { this.ws.close(); } catch { /* ignore */ }
  }

  async _loop() {
    while (!this.closed) {
      const msg = await this.recv.next();
      if (msg === null) { this.closed = true; return; }
      if (msg.type !== T.IncomingQuery) continue; // ignore stray frames
      const q = new IncomingQuery(this, msg.value);
      // fire-and-forget; the handler decides accept/reject. Errors are swallowed
      // here — callers should handle their own.
      Promise.resolve()
        .then(() => this.handler(q))
        .catch((err) => {
          try { q.reject(0xff); } catch { /* ignore */ }
          // Surface the error on the stream WS console for debugging.
          // eslint-disable-next-line no-console
          console.error('apphost handler error:', err);
        });
    }
  }
}

export class IncomingQuery {
  /**
   * @param {Registration} reg
   * @param {{QueryID: string, Caller: string|null, Target: string, Query: string}} raw
   */
  constructor(reg, raw) {
    this._reg = reg;
    this.id = raw.QueryID;
    this.caller = raw.Caller || null;
    this.target = raw.Target;
    // Split the full query string into the path (e.g. "chat.send") and a
    // friendly params object — the apphost may inject ?in=json&out=json so a
    // bare `query === 'chat.send'` check would otherwise fail.
    const full = raw.Query || '';
    const i = full.indexOf('?');
    this.query = i < 0 ? full : full.slice(0, i);
    this.params = {};
    if (i >= 0) {
      // A repeated key keeps its first value, as astral-go's query.Parse does.
      for (const [key, value] of new URLSearchParams(full.slice(i + 1))) {
        if (!Object.hasOwn(this.params, key)) this.params[key] = value;
      }
    }
    this.queryString = full;
    this._settled = false;
  }

  /** Accept the query: opens a per-query WS, attaches, returns a responder Stream. */
  async accept() {
    if (this._settled) throw new Error('incoming query already settled');
    this._settled = true;

    // Per-query WSes do not need auth — the QueryID is the pairing token.
    const { ws, recv } = await openSession(this._reg.host.url, this._reg.host.token, { skipAuth: true });
    rawSend(ws, wrap({ type: T.AttachQuery, value: { QueryID: this.id } }));

    const resp = await recv.next();
    if (!resp) { ws.close(); throw new ConnectError('socket closed during attach'); }
    if (resp.type === T.Error) { ws.close(); throw new ProtocolError('attach failed: ' + (resp.value && resp.value.Code)); }
    if (resp.type !== T.Ack) { ws.close(); throw new ProtocolError('expected ack, got ' + resp.type); }

    return new Stream(ws, recv);
  }

  /** Reject the query with a numeric code (1-255). */
  reject(code = 1) {
    if (this._settled) throw new Error('incoming query already settled');
    this._settled = true;
    if (this._reg.closed) return;
    rawSend(this._reg.ws, wrap({ type: T.RejectIncoming, value: { QueryID: this.id, Code: code } }));
  }
}
