package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	apphostClient "github.com/cryptopunkscc/astral-go/api/apphost/client"
	dirClient "github.com/cryptopunkscc/astral-go/api/dir/client"
	objectsClient "github.com/cryptopunkscc/astral-go/api/objects/client"
	"github.com/cryptopunkscc/astral-go/api/secp256k1"
	secp256k1Client "github.com/cryptopunkscc/astral-go/api/secp256k1/client"
	"github.com/cryptopunkscc/astral-go/lib/apphost"
	"github.com/cryptopunkscc/astral-go/lib/astrald"
)

func main() {
	var asFlag string
	var ensureIdentityFlag bool
	var ensureTokenFlag bool
	var extraEnv envList

	flag.StringVar(&asFlag, "as", "", "identity or `alias` to run as")
	flag.BoolVar(&ensureTokenFlag, "ensure-token", false, "reuse existing token or create one if none exists")
	flag.BoolVar(&ensureIdentityFlag, "ensure-identity", false, "if alias not found in dir, create identity and register it; implies -ensure-token")
	flag.Var(&extraEnv, "env", "set an environment variable `KEY=VALUE` (repeatable)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s -as <identity|alias> [-ensure-identity] [-ensure-token] [-env KEY=VALUE]... <executable> [args...]\n\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if asFlag == "" {
		flag.Usage()
		os.Exit(1)
	}
	if flag.NArg() < 1 {
		fatal("error: executable not specified")
	}

	if ensureIdentityFlag {
		ensureTokenFlag = true
	}

	ctx := astrald.NewContext()

	identity, err := dirClient.ResolveIdentity(ctx, asFlag)
	if err != nil {
		if !ensureIdentityFlag {
			fatal("error: resolve %q: %v", asFlag, err)
		}

		privateKey, err := secp256k1Client.NewKey(ctx)
		if err != nil {
			fatal("error: generate key: %v", err)
		}

		// todo: get repository name
		if _, err = objectsClient.Default().Store(ctx, "", privateKey); err != nil {
			fatal("error: store key: %v", err)
		}

		identity = secp256k1.Identity(secp256k1.PublicKey(privateKey))
		if err = dirClient.SetAlias(ctx, identity, asFlag); err != nil {
			fatal("error: register alias %q: %v", asFlag, err)
		}

		fmt.Fprintf(os.Stderr, "created identity %v as %q\n", identity, asFlag)
	}

	tokens, err := apphostClient.ListTokens(ctx, identity)
	if err != nil {
		fatal("error: list tokens: %v", err)
	}
	if len(tokens) == 0 && !ensureTokenFlag {
		fatal("error: no token found for identity (use --ensure-token to create one)")
	}

	if len(tokens) == 0 {
		t, err := apphostClient.CreateToken(ctx, identity)
		if err != nil {
			fatal("error: create token: %v", err)
		}
		tokens = append(tokens, t)
	}
	tokenStr := tokens[0].Token

	path, err := exec.LookPath(flag.Arg(0))
	if err != nil {
		fatal("error: %v", err)
	}

	argv := append([]string{flag.Arg(0)}, flag.Args()[1:]...)
	env := append(stripEnv(os.Environ(), apphost.AuthTokenEnv), fmt.Sprintf("%s=%s", apphost.AuthTokenEnv, tokenStr))
	env = append(env, extraEnv...)

	if err = syscall.Exec(path, argv, env); err != nil {
		fatal("error: exec: %v", err)
	}
}

// stripEnv returns a copy of env with all entries for the given key removed.
func stripEnv(env []string, key string) []string {
	prefix := key + "="
	out := env[:0:len(env)]
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return out
}

type envList []string

func (e *envList) String() string { return strings.Join(*e, " ") }

func (e *envList) Set(s string) error {
	if !strings.Contains(s, "=") {
		return fmt.Errorf("invalid env format: %s", s)
	}
	*e = append(*e, s)
	return nil
}
func fatal(f string, v ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", v...)
	os.Exit(1)
}
