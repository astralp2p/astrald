# read-remote-object

node1's agent reads node2's Object (id from `~/object.json`) over astral as the User and records it to `~/read.json`. verify.py independently re-reads it via `node2:objects.load` as the User and asserts the bytes equal node1's stored `payload.txt`.
