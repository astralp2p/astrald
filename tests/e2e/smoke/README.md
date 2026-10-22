# smoke

Start state: null (one fresh node). The driver asks the node anonymously for
`user.info`, the claim-state probe every token-less session reaches, and for
`shell.spec`, which the anonymous allowlist refuses. The oracle checks both
answers, that the seeded node token authenticates, that the op catalog carries
the core ops, and that `apphost.whoami` returns the node identity.
