package vfs_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	apiauth "github.com/astralp2p/astral-go/api/auth"
	objectsapi "github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/api/secp256k1"
	userapi "github.com/astralp2p/astral-go/api/user"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	apphostclient "github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/ipc"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astrald/core"
	_ "github.com/astralp2p/astrald/mod/all"
	authmod "github.com/astralp2p/astrald/mod/auth"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	usermod "github.com/astralp2p/astrald/mod/user"
	"github.com/astralp2p/astrald/resources"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// note: an acceptance test runs one node in the process: the modules below, wired by the module manager as
// astrald wires them, with the default fs repository in the group local, apphost listening in memory and the
// HTTP API on. An app reaches the node through the apphost client library, as an app does.

// acceptanceModules is the smallest module set that loads: the modules the VFS ticket touches and their
// dependencies.
const acceptanceModules = "[apphost, auth, crypto, dir, ether, events, exonet, fs, nearby, nodes, objects, " +
	"scheduler, secp256k1, shell, sys, tree, user, vfs]"

// acceptanceWait bounds every wait of an acceptance test.
const acceptanceWait = 10 * time.Second

// acceptanceListeners numbers the in-memory listeners, which share one namespace in a process.
var acceptanceListeners atomic.Uint64

// acceptanceNode is a running node and the means to reach it.
type acceptanceNode struct {
	t        *testing.T
	root     string
	id       *astral.Identity
	node     *core.Node
	ctx      *astral.Context
	stop     func()
	endpoint string   // the apphost IPC endpoint
	httpAddr string   // the apphost HTTP endpoint
	db       *gorm.DB // a second connection to the node's database
}

// startAcceptanceNode starts a node in a temporary root and returns once apphost answers and the group local
// has a member.
func startAcceptanceNode(t *testing.T) *acceptanceNode {
	t.Helper()

	a := &acceptanceNode{t: t, root: t.TempDir()}

	key := secp256k1.New()
	a.id = secp256k1.Identity(secp256k1.PublicKey(key))
	acceptanceWriteKey(t, a.resources(), key)

	a.run()
	a.db = acceptanceOpenDB(t, filepath.Join(a.root, "data", "astrald.db"))

	return a
}

// run starts the node on its root with new listeners and waits until it is ready. The node stops when the test
// ends or on restart.
func (a *acceptanceNode) run() {
	a.t.Helper()

	n := acceptanceListeners.Add(1)
	a.endpoint = fmt.Sprintf("memu:vfs-acceptance-%d", n)
	a.httpAddr = fmt.Sprintf("memu:vfs-acceptance-http-%d", n)
	a.writeConfig()

	node, err := core.NewNode(a.id, a.resources())
	if err != nil {
		a.t.Fatalf("new node: %v", err)
	}
	a.node = node

	ctx, cancel := astral.NewContext(nil).WithCancel()
	a.ctx = ctx
	done := make(chan error, 1)
	go func() { done <- node.Run(ctx) }()

	var stopped atomic.Bool
	a.stop = func() {
		if stopped.CompareAndSwap(false, true) {
			cancel()
			<-done
		}
	}
	a.t.Cleanup(a.stop)

	a.waitReady(done)
}

// restart stops the node and starts it again on the same root.
func (a *acceptanceNode) restart() {
	a.t.Helper()

	a.stop()
	a.run()
}

// writeConfig writes the config files of the node.
// note: the daily purge task is off, so only the purges a test runs delete objects.
func (a *acceptanceNode) writeConfig() {
	a.t.Helper()

	configs := map[string]string{
		"node.yaml":    "modules: " + acceptanceModules + "\n",
		"apphost.yaml": fmt.Sprintf("listen: [%q]\nbind_http: %q\npolicies:\n  app_register: accept_all\n", a.endpoint, a.httpAddr),
		"objects.yaml": "purge_untouched: 0\n",
	}
	for name, body := range configs {
		if err := os.WriteFile(filepath.Join(a.root, name), []byte(body), 0600); err != nil {
			a.t.Fatalf("write %v: %v", name, err)
		}
	}
}

// resources returns the resources of the node: the config in the root, the data in root/data.
func (a *acceptanceNode) resources() *resources.FileResources {
	a.t.Helper()

	res, err := resources.NewFileResources(a.root, false)
	if err != nil {
		a.t.Fatalf("resources: %v", err)
	}
	dataRoot := filepath.Join(a.root, "data")
	if err := os.MkdirAll(dataRoot, 0700); err != nil {
		a.t.Fatalf("mkdir data: %v", err)
	}
	if err := res.SetDataRoot(dataRoot); err != nil {
		a.t.Fatalf("data root: %v", err)
	}

	return res
}

// acceptanceWriteKey stores the node key where the crypto module loads it from.
func acceptanceWriteKey(t *testing.T, res *resources.FileResources, key astral.Object) {
	t.Helper()

	var buf bytes.Buffer
	if _, err := astral.Encode(&buf, key, astral.Canonical()); err != nil {
		t.Fatalf("encode node key: %v", err)
	}
	if err := res.WriteData("node_key", buf.Bytes()); err != nil {
		t.Fatalf("write node key: %v", err)
	}
}

// acceptanceOpenDB opens a second connection to the node's database.
func acceptanceOpenDB(t *testing.T, path string) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(path+"?_pragma=busy_timeout(10000)"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open %v: %v", path, err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})

	return db
}

// waitReady waits until apphost accepts a session and the group local has a member.
func (a *acceptanceNode) waitReady(done <-chan error) {
	a.t.Helper()

	deadline := time.Now().Add(acceptanceWait)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			a.t.Fatalf("the node ended: %v", err)
		default:
		}

		host, err := apphostclient.Connect(a.ctx, a.endpoint)
		if err == nil {
			host.Close()
			if len(a.members(objectsapi.RepoLocal)) > 0 {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	a.t.Fatal("the node did not become ready")
}

// members returns the names of the members of a group.
func (a *acceptanceNode) members(group string) []string {
	if g, ok := a.objects().GetRepository(group).(interface{ List() []string }); ok {
		return g.List()
	}

	return nil
}

func (a *acceptanceNode) nodeID() *astral.Identity { return a.id }

func (a *acceptanceNode) objects() objectsmod.Module {
	return a.node.Modules().Find("objects").(objectsmod.Module)
}

func (a *acceptanceNode) crypto() cryptomod.Module {
	return a.node.Modules().Find("crypto").(cryptomod.Module)
}

func (a *acceptanceNode) auth() authmod.Module {
	return a.node.Modules().Find("auth").(authmod.Module)
}

// acceptanceUsers is the user module with the method that sets the active contract.
type acceptanceUsers interface {
	usermod.Module
	SetActiveContract(*apiauth.SignedContract) error
}

// claim makes a new user the owner of the node, as user.claim does: the user's key is stored in local and indexed,
// and the node contract signed by both is the active contract. It returns once the user module names the user.
func (a *acceptanceNode) claim() *astral.Identity {
	a.t.Helper()

	key := secp256k1.New()
	userID := secp256k1.Identity(secp256k1.PublicKey(key))
	if _, err := a.objects().Store(a.ctx, a.objects().WriteDefault(), key); err != nil {
		a.t.Fatalf("store the user key: %v", err)
	}

	contract, err := userapi.NewNodeContract(userID, a.nodeID(), false, 24*time.Hour)
	if err != nil {
		a.t.Fatalf("node contract: %v", err)
	}
	signed := &apiauth.SignedContract{Contract: contract}
	if err = a.auth().SignContract(a.ctx, signed); err != nil {
		a.t.Fatalf("sign the node contract: %v", err)
	}
	if _, err = a.objects().Store(a.ctx, a.objects().WriteDefault(), signed); err != nil {
		a.t.Fatalf("store the node contract: %v", err)
	}

	users := a.node.Modules().Find("user").(acceptanceUsers)
	if err = users.SetActiveContract(signed); err != nil {
		a.t.Fatalf("set the active contract: %v", err)
	}
	a.eventually("the user module names the user", func() bool { return userID.IsEqual(users.Identity()) })

	return userID
}

// eventually waits until cond holds.
func (a *acceptanceNode) eventually(what string, cond func() bool) {
	a.t.Helper()

	deadline := time.Now().Add(acceptanceWait)
	for !cond() {
		if time.Now().After(deadline) {
			a.t.Fatalf("timed out: %v", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// acceptanceApp is a registered app: its identity, token and client.
type acceptanceApp struct {
	id     *astral.Identity
	token  string
	client *astrald.Client
}

// registerApp registers a new app through apphost.register in a session without a token, as an app does.
func (a *acceptanceNode) registerApp() *acceptanceApp {
	a.t.Helper()

	ch := a.mustChannel(a.tokenless(), "apphost.register", nil)
	if err := ch.Send(&apphostapi.RegisterRequest{Name: "acceptance"}); err != nil {
		a.t.Fatalf("send the register request: %v", err)
	}

	answer, err := ch.Receive()
	if err != nil {
		a.t.Fatalf("apphost.register: %v", err)
	}
	token, ok := answer.(*apphostapi.AccessToken)
	if !ok {
		a.t.Fatalf("apphost.register answered %#v", answer)
	}

	return &acceptanceApp{id: token.Identity, token: string(token.Token), client: a.withToken(string(token.Token))}
}

// withToken returns a client of an apphost session authenticated with token.
func (a *acceptanceNode) withToken(token string) *astrald.Client {
	return astrald.New(apphostclient.NewRouter(a.endpoint, token)).WithTarget(a.nodeID())
}

// tokenless returns a client of an apphost session without a token.
func (a *acceptanceNode) tokenless() *astrald.Client { return a.withToken("") }

// as returns a client that routes a query inside the node with caller as the caller, as a module of the node
// routes a local query.
func (a *acceptanceNode) as(caller *astral.Identity) *astrald.Client {
	return core.NewClientAs(a.node, caller).WithTarget(a.nodeID())
}

// fromNetwork routes a query inside the node marked as arrived over a link from caller.
func (a *acceptanceNode) fromNetwork(caller *astral.Identity, method string, args any) (*channel.Channel, error) {
	q := astral.Launch(query.New(caller, a.nodeID(), method, args))
	q.Extra.Set("origin", astral.OriginNetwork)

	ctx, cancel := a.ctx.WithTimeout(acceptanceWait)
	a.t.Cleanup(cancel)

	conn, err := a.as(caller).RouteQuery(ctx, q)
	if err != nil {
		return nil, err
	}
	ch := channel.New(conn)
	a.t.Cleanup(func() { ch.Close() })

	return ch, nil
}

// channel opens a query as a channel. The channel closes when the test ends.
func (a *acceptanceNode) channel(c *astrald.Client, method string, args any) (*channel.Channel, error) {
	ctx, cancel := a.ctx.WithTimeout(acceptanceWait)
	a.t.Cleanup(cancel)

	ch, err := c.QueryChannel(ctx, method, args)
	if err != nil {
		return nil, err
	}
	// why: a receive has no deadline of its own, so the channel closes when the wait runs out.
	context.AfterFunc(ctx, func() { ch.Close() })
	a.t.Cleanup(func() { ch.Close() })

	return ch, nil
}

func (a *acceptanceNode) mustChannel(c *astrald.Client, method string, args any) *channel.Channel {
	a.t.Helper()

	ch, err := a.channel(c, method, args)
	if err != nil {
		a.t.Fatalf("%v: %v", method, err)
	}

	return ch
}

// call opens a query and returns its first answer, or the error of the query or of the receive.
func (a *acceptanceNode) call(c *astrald.Client, method string, args any) (astral.Object, error) {
	ch, err := a.channel(c, method, args)
	if err != nil {
		return nil, err
	}

	return ch.Receive()
}

// readRaw opens a raw query and returns every byte it answers. A rejected query returns its error.
func (a *acceptanceNode) readRaw(c *astrald.Client, method string, args any) ([]byte, error) {
	ctx, cancel := a.ctx.WithTimeout(acceptanceWait)
	defer cancel()

	conn, err := c.Query(ctx, method, args)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	content, err := io.ReadAll(conn)
	// note: the in-memory connection wraps the end of the stream in a net.OpError.
	if errors.Is(err, io.EOF) {
		err = nil
	}

	return content, err
}

// write writes content to a file through vfs.write and returns the answer after the commit, or the first answer
// when it is not an ack.
func (a *acceptanceNode) write(c *astrald.Client, args query.Args, content []byte) (astral.Object, error) {
	ch, err := a.channel(c, "vfs.write", args)
	if err != nil {
		return nil, err
	}

	first, err := ch.Receive()
	if err != nil {
		return nil, err
	}
	if _, ok := first.(*astral.Ack); !ok {
		return first, nil
	}

	if len(content) > 0 {
		blob := astral.Blob(content)
		if err = ch.Send(&blob); err != nil {
			return nil, err
		}
	}
	if err = ch.Send(&objectsapi.CommitMsg{}); err != nil {
		return nil, err
	}

	return ch.Receive()
}

// mustWrite writes a file of the caller's own VFS and returns its entry.
func (a *acceptanceNode) mustWrite(c *astrald.Client, path string, content []byte) *vfsapi.Entry {
	a.t.Helper()

	answer, err := a.write(c, query.Args{"path": path}, content)
	if err != nil {
		a.t.Fatalf("vfs.write %v: %v", path, err)
	}
	entry, ok := answer.(*vfsapi.Entry)
	if !ok {
		a.t.Fatalf("vfs.write %v answered %#v", path, answer)
	}

	return entry
}

// httpGet fetches a path from the apphost HTTP API with a token and returns the status and the body.
func (a *acceptanceNode) httpGet(token, path string) (int, []byte) {
	a.t.Helper()

	client := &http.Client{
		Timeout: acceptanceWait,
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return ipc.DialContext(ctx, a.httpAddr)
		}},
	}

	req, err := http.NewRequest(http.MethodGet, "http://apphost"+path, nil)
	if err != nil {
		a.t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		a.t.Fatalf("GET %v: %v", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		a.t.Fatalf("GET %v body: %v", path, err)
	}

	return resp.StatusCode, body
}

// acceptanceTracking is a row of the tracking table.
type acceptanceTracking struct {
	Repository  string
	CommittedAt int64
	ReadAt      int64
}

// tracking returns the tracking rows of an object, one per leaf that has a copy, by leaf name.
func (a *acceptanceNode) tracking(id *astral.ObjectID) map[string]acceptanceTracking {
	a.t.Helper()

	var rows []acceptanceTracking
	err := a.db.Table(objectsmod.DBPrefix+"tracking").Where("object_id = ?", id.String()).Find(&rows).Error
	if err != nil {
		a.t.Fatalf("tracking rows of %v: %v", id, err)
	}

	byLeaf := map[string]acceptanceTracking{}
	for _, row := range rows {
		byLeaf[row.Repository] = row
	}

	return byLeaf
}

// age moves the commit and read times of every tracking row back by d, as if d had passed since.
func (a *acceptanceNode) age(d time.Duration) {
	a.t.Helper()

	s := int64(d / time.Second)
	err := a.db.Exec("UPDATE "+objectsmod.DBPrefix+"tracking SET committed_at = committed_at - ?, "+
		"read_at = CASE WHEN read_at > 0 THEN read_at - ? ELSE 0 END", s, s).Error
	if err != nil {
		a.t.Fatalf("age the tracking rows: %v", err)
	}
}

// purge runs objects.purge as the node and returns the ObjectIDs it answers.
func (a *acceptanceNode) purge(repo string, untouched time.Duration, dry bool) []string {
	a.t.Helper()

	args := query.Args{"repo": repo, "dry": dry}
	if untouched > 0 {
		args["untouched"] = untouched.String()
	}

	ch := a.mustChannel(a.as(a.nodeID()), "objects.purge", args)

	var purged []string
	for {
		answer, err := ch.Receive()
		if err != nil {
			a.t.Fatalf("objects.purge: %v", err)
		}
		switch answer := answer.(type) {
		case *astral.ObjectID:
			purged = append(purged, answer.String())
		case *astral.EOS:
			return purged
		default:
			a.t.Fatalf("objects.purge answered %#v", answer)
		}
	}
}

// flushReads writes the reads journal: a dry purge flushes it and deletes nothing.
func (a *acceptanceNode) flushReads() {
	a.t.Helper()

	a.purge(objectsapi.RepoDevice, 0, true)
}

// contains reports whether a repository has an object.
func (a *acceptanceNode) contains(repo string, id *astral.ObjectID) bool {
	a.t.Helper()

	has, err := a.objects().GetRepository(repo).Contains(a.ctx, id)
	if err != nil {
		return false
	}

	return has
}

// localLeaf returns the name of the one member of the group local: the default fs repository.
func (a *acceptanceNode) localLeaf() string {
	a.t.Helper()

	members := a.members(objectsapi.RepoLocal)
	if len(members) != 1 {
		a.t.Fatalf("local has members %v; want one", members)
	}

	return members[0]
}
