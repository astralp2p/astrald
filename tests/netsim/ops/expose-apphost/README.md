# expose-apphost

Gives the harness one door into a NAT'd node's network namespace.

`enter-nat` relocates astrald into netns `priv`, where apphost binds that
namespace's `127.0.0.1`. The session is an `ssh -L` forward and ssh lands in
the VM's root netns, so after `enter-nat` a NAT'd node's apphost is
unreachable from the host: a driver connects to the forward and meets
`closed before the greeting`.

This starts a relay **inside** `priv`, bound to the namespace's own veth
address (`192.168.99.2:8625`) and forwarding to the namespace loopback. The
root netns can reach that address over the veth pair `enter-nat` created, and
nothing else can.

The alternative — binding apphost to the LAN, or punching the netns open —
would dissolve the thing under test. These nodes are behind symmetric NAT with
no direct path, and that is the point of `nat-punch`. A door for the harness
is not a door for the network.

Idempotent: a second run finds the listener and returns.

    expose-apphost [--vm <host>]...    # default: every running VM in priv
