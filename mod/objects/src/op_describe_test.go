package objects

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
)

// descriptorOf wraps data in a descriptor, as a describer would.
func descriptorOf(data astral.Object) *objects.Descriptor {
	return &objects.Descriptor{
		SourceID: &astral.Identity{},
		ObjectID: &astral.ObjectID{},
		Data:     data,
	}
}

// feedDescriptors returns a closed channel carrying the given descriptors, standing in
// for Module.Describe's merged describer output.
func feedDescriptors(descriptors ...*objects.Descriptor) <-chan *objects.Descriptor {
	out := make(chan *objects.Descriptor, len(descriptors))
	for _, d := range descriptors {
		out <- d
	}
	close(out)
	return out
}

// runDescribe drives streamDescriptors over a recordingSender and returns the payload
// types it relayed, excluding the terminating EOS.
func runDescribe(t *testing.T, args opDescribeArgs, in <-chan *objects.Descriptor) []string {
	t.Helper()

	snd := &recordingSender{}
	ch := &channel.Channel{Sender: snd}

	done := make(chan error, 1)
	go func() { done <- streamDescriptors(astral.NewContext(nil), ch, in, args) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("streamDescriptors returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("streamDescriptors did not return")
	}

	if len(snd.sent) == 0 {
		t.Fatalf("streamDescriptors sent nothing; want at least an EOS")
	}
	last := snd.sent[len(snd.sent)-1]
	if got := last.ObjectType(); got != (astral.EOS{}).ObjectType() {
		t.Fatalf("stream ended with %q; want EOS", got)
	}

	var types []string
	for _, o := range snd.sent[:len(snd.sent)-1] {
		d, ok := o.(*objects.Descriptor)
		if !ok {
			t.Fatalf("relayed %T; want *objects.Descriptor", o)
		}
		types = append(types, d.Data.ObjectType())
	}
	return types
}

func str(s string) *string { return &s }

func s8(v string) *astral.String8 { o := astral.String8(v); return &o }

func u64(v uint64) *astral.Uint64 { o := astral.Uint64(v); return &o }

// TestStreamDescriptors_OnlyMatchesDescribedType pins the intended filter subject: Only
// names the type of the object being described, not the descriptor wrapper. The bug
// filtered on Descriptor.ObjectType(), the constant "mod.objects.describe_result", so
// Only=<any real type> matched nothing and silently returned an empty stream.
func TestStreamDescriptors_OnlyMatchesDescribedType(t *testing.T) {
	in := feedDescriptors(
		descriptorOf(astral.NewError("payload")),
		descriptorOf(s8("keep me")),
	)

	got := runDescribe(t, opDescribeArgs{Only: str(s8("").ObjectType())}, in)

	want := []string{s8("").ObjectType()}
	if !equalStrings(got, want) {
		t.Fatalf("Only=%q relayed %v; want %v", s8("").ObjectType(), got, want)
	}
}

// TestStreamDescriptors_ExceptMatchesDescribedType is the Except mirror: the bug never
// matched the wrapper constant, so Except was a no-op that filtered nothing.
func TestStreamDescriptors_ExceptMatchesDescribedType(t *testing.T) {
	in := feedDescriptors(
		descriptorOf(astral.NewError("drop me")),
		descriptorOf(s8("keep me")),
	)

	got := runDescribe(t, opDescribeArgs{Except: str(astral.NewError("").ObjectType())}, in)

	want := []string{s8("").ObjectType()}
	if !equalStrings(got, want) {
		t.Fatalf("Except=%q relayed %v; want %v", astral.NewError("").ObjectType(), got, want)
	}
}

// TestStreamDescriptors_WrapperTypeIsNotAFilterKey guards the inverse of the bug: the
// descriptor wrapper's own type must not select anything, or Only would keep behaving
// as an all-or-nothing switch.
func TestStreamDescriptors_WrapperTypeIsNotAFilterKey(t *testing.T) {
	in := feedDescriptors(
		descriptorOf(astral.NewError("payload")),
		descriptorOf(s8("payload")),
	)

	got := runDescribe(t, opDescribeArgs{Only: str((objects.Descriptor{}).ObjectType())}, in)

	if len(got) != 0 {
		t.Fatalf("Only=%q relayed %v; want nothing", (objects.Descriptor{}).ObjectType(), got)
	}
}

// TestStreamDescriptors_NoFilterRelaysAll checks the unfiltered path still passes
// everything through.
func TestStreamDescriptors_NoFilterRelaysAll(t *testing.T) {
	in := feedDescriptors(
		descriptorOf(astral.NewError("a")),
		descriptorOf(s8("b")),
	)

	got := runDescribe(t, opDescribeArgs{}, in)

	want := []string{astral.NewError("").ObjectType(), s8("").ObjectType()}
	if !equalStrings(got, want) {
		t.Fatalf("unfiltered stream relayed %v; want %v", got, want)
	}
}

// TestStreamDescriptors_MultipleTypes covers the comma-separated list, since Only and
// Except are split on commas.
func TestStreamDescriptors_MultipleTypes(t *testing.T) {
	in := feedDescriptors(
		descriptorOf(astral.NewError("a")),
		descriptorOf(s8("b")),
		descriptorOf(u64(7)),
	)

	only := astral.NewError("").ObjectType() + "," + u64(0).ObjectType()
	got := runDescribe(t, opDescribeArgs{Only: str(only)}, in)

	want := []string{astral.NewError("").ObjectType(), u64(0).ObjectType()}
	if !equalStrings(got, want) {
		t.Fatalf("Only=%q relayed %v; want %v", only, got, want)
	}
}

// TestStreamDescriptors_NilDataDoesNotPanic covers descriptors from third-party
// describers registered via Module.AddDescriber: reading the described type must not
// dereference a nil Data.
func TestStreamDescriptors_NilDataDoesNotPanic(t *testing.T) {
	snd := &recordingSender{}
	ch := &channel.Channel{Sender: snd}
	in := feedDescriptors(&objects.Descriptor{})

	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("streamDescriptors panicked on nil Data: %v", r)
				done <- nil
			}
		}()
		done <- streamDescriptors(astral.NewContext(nil), ch, in, opDescribeArgs{Only: str("anything")})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("streamDescriptors returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("streamDescriptors did not return")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
