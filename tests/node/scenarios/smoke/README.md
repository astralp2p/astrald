# smoke

Start state: null (one fresh node). The driver queries the op catalog
anonymously; the oracle checks the catalog carries the core ops, the seeded
node token authenticates, and `apphost.whoami` returns the node identity.
