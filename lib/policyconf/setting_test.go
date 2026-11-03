package policyconf

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"gopkg.in/yaml.v2"
)

type block struct {
	A Setting `yaml:"a"`
	B Setting `yaml:"b"`
}

func TestSettingReadsModesAndDelegates(t *testing.T) {
	var got block
	src := "a: accept_all\nb: {identity: cp, operation: cp.approve, timeout: 30s}\n"
	if err := yaml.UnmarshalStrict([]byte(src), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.A.Accepts() {
		t.Fatalf("a = %+v; want accept_all", got.A)
	}
	want := DelegateSetting{Identity: "cp", Operation: "cp.approve", Timeout: 30 * time.Second}
	if got.B.Delegate == nil || *got.B.Delegate != want {
		t.Fatalf("b = %+v; want delegate %+v", got.B, want)
	}
}

func TestSettingKeepsOmittedKeys(t *testing.T) {
	got := block{A: Setting{Mode: AcceptAll}, B: Setting{Mode: AcceptAll}}
	if err := yaml.UnmarshalStrict([]byte("b: reject_all\n"), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.A.Accepts() || got.B.Accepts() {
		t.Fatalf("got %+v; want a kept accept_all, b reject_all", got)
	}
}

func TestSettingRefusesInvalidValues(t *testing.T) {
	for _, src := range []string{
		"a: allow\n",
		"a: {identity: cp}\n",
		"a: {operation: cp.approve}\n",
		"a: {identity: cp, operation: x, timeout: 11m}\n",
		"a: {identity: cp, operation: x, other: 1}\n",
	} {
		var got block
		if err := yaml.UnmarshalStrict([]byte(src), &got); err == nil {
			t.Errorf("%q: accepted as %+v", src, got)
		}
	}
}

func TestZeroSettingRejects(t *testing.T) {
	if (Setting{}).Accepts() {
		t.Fatal("zero Setting accepts")
	}
	d, err := (Setting{}).ResolveDelegate(nil)
	if d != nil || err != nil {
		t.Fatalf("zero Setting delegate = %v, %v; want nil, nil", d, err)
	}
}

func TestResolveDelegate(t *testing.T) {
	id := astral.GenerateIdentity()
	s := Setting{Delegate: &DelegateSetting{Identity: "cp", Operation: "cp.approve"}}
	d, err := s.ResolveDelegate(func(name string) (*astral.Identity, error) { return id, nil })
	if err != nil || !d.Identity.IsEqual(id) || d.Operation != "cp.approve" {
		t.Fatalf("delegate = %+v, %v", d, err)
	}
}
