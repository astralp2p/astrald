# import-user-software-key.story — make node1 a User node from an EXISTING mnemonic
# (embedded in the task's prompt.md; alternative to bootstrap-user-software-key).
# Optional env ASTRAL_USER_ID makes verify assert the derived id.
# start: astrald-lab   save: one-node
#   netsim story --stage astrald-lab --save one-node netsim/scenarios/import-user-software-key/import-user-software-key.story
import-user-software-key
