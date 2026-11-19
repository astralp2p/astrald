"""Identities of nodes that do not exist, for endpoint rows nothing answers.

A stranger is a valid compressed secp256k1 public key (66 hex digits), so a
node accepts it as a link target and as a gateway in a `gw:` row, while no
daemon holds its private key. Each call draws a fresh scalar, so strangers of
different tests never meet in one node's book.

why plain Python: the curve arithmetic for one public key is a few lines, and
the harness needs no crypto package for it.
"""
import secrets

_P = 2**256 - 2**32 - 977
_N = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141
_G = (0x79BE667EF9DCBBAC55A06295CE870B07029BFCDB2DCE28D959F2815B16F81798,
      0x483ADA7726A3C4655DA4FBFC0E1108A8FD17B448A68554199C47D08FFB10D4B8)


def _add(a, b):
    if a is None:
        return b
    if b is None:
        return a
    if a[0] == b[0] and (a[1] + b[1]) % _P == 0:
        return None
    if a == b:
        m = 3 * a[0] * a[0] * pow(2 * a[1], -1, _P)
    else:
        m = (b[1] - a[1]) * pow(b[0] - a[0], -1, _P)
    x = (m * m - a[0] - b[0]) % _P
    return x, (m * (a[0] - x) - a[1]) % _P


def _mul(k: int, point):
    out = None
    while k:
        if k & 1:
            out = _add(out, point)
        point = _add(point, point)
        k >>= 1
    return out


def stranger() -> str:
    """A fresh identity no node holds, as 66 hex digits."""
    x, y = _mul(secrets.randbelow(_N - 1) + 1, _G)
    return ("03" if y & 1 else "02") + format(x, "064x")
