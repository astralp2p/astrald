"""lib/nearby.summary: a mod.nearby.status as the facts the nearby tests judge."""
import unittest

from lib import nearby

NODE = "02" + "ab" * 32


def status(*attachments) -> dict:
    return {"Identity": NODE, "Attachments": [
        {"Type": t, "Object": o} for t, o in attachments]}


class TestSummary(unittest.TestCase):
    def test_a_visible_status_gives_its_plain_ports(self):
        s = nearby.summary(status(
            (nearby.FLAG, "adoptable"),
            (nearby.PROFILE, {"NodeID": NODE, "NodeAlias": "n2"}),
            (nearby.LISTEN_PORTS, {"TCP": 1800, "KCP": 1810}),
            (nearby.PROOF, {"Nonce": 7})))
        self.assertEqual(s["ports"], {"tcp": 1800, "kcp": 1810})
        self.assertEqual(s["types"], [nearby.FLAG, nearby.PROFILE,
                                      nearby.LISTEN_PORTS, nearby.PROOF])

    def test_sealed_ports_or_none_give_no_plain_ports(self):
        # why: sealed ports open only with the node identity, which the
        # listing does not hand a driver; their type alone is the fact
        sealed = nearby.summary(status(
            (nearby.HINT, {}), (nearby.SEALED_PORTS, {"Data": "AAAAAA=="}),
            (nearby.PROOF, {})))
        self.assertIsNone(sealed["ports"])
        self.assertIn(nearby.SEALED_PORTS, sealed["types"])
        self.assertIsNone(nearby.summary(status((nearby.PROOF, {})))["ports"])


if __name__ == "__main__":
    unittest.main()
