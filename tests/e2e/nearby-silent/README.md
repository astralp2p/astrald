# nearby-silent

A node in nearby's Silent mode sends no ether datagram, whatever asks it to,
and still hears its peers.

- **start** `nearby-stealth-meet`: `node1`, `node2` and the reflector are the
  User's, broadcasting stealth.
- **roster** `node1`, `node2`, `reflector`.
- **op** `capture-ether reflector --for 150 --name silent`: a capture of UDP
  8822 on every interface of the reflector, which writes its record to the
  reflector's tree at `/e2e/capture/silent` when its window closes.
- **driver** `script.py`: with the reflector's log recorded, stores Silent as
  its mode (`tree.set /mod/nearby/mode`), calls `nearby.broadcast` and
  `nearby.scan` on the reflector, has `node1` and `node2` scan, waits past one
  status interval, has them scan again, and reads the capture's record.
- **oracle** `verify.py`: the reflector logged `mode 0`; no datagram left it
  later than half a second after that line, in a capture that holds the switch
  and at least 100 s after it and that heard `node1` and `node2` in that time;
  its own `nearby.broadcast` and `nearby.scan` answered `ack`; `node1` and
  `node2` kept the reflector's last status from before the switch while each
  refreshed the other after its scan; the reflector refreshed both.

The capture is the judge of "sends nothing": it runs on the reflector's own
interfaces, so it sees a datagram no peer would receive or keep, a scan among
them. The lists are the astral-level view of the same silence, and the
controls show the window held traffic the reflector would have answered.

Not covered: an address change while Silent (the same `Broadcast` and `Scan`
calls the driver triggers), a node Silent from its start, and `silent_listen`.
