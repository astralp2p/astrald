package nodes

import (
	"testing"
	"time"
)

// TestRecordRTTSmooths: the first pong sets the smoothed round trip; a later pong moves it by an eighth of the difference.
func TestRecordRTTSmooths(t *testing.T) {
	link := &Link{createdAt: time.Now()}

	link.recordRTT(100*time.Millisecond, link.createdAt.Add(time.Second))
	if got := time.Duration(link.srtt.Load()); got != 100*time.Millisecond {
		t.Fatalf("srtt after the first pong = %v; want 100ms", got)
	}

	link.recordRTT(20*time.Millisecond, link.createdAt.Add(2*time.Second))
	if got := time.Duration(link.srtt.Load()); got != 90*time.Millisecond {
		t.Fatalf("srtt after 100ms then 20ms = %v; want 90ms", got)
	}
	if got := time.Duration(link.lastPong.Load()); got != 2*time.Second {
		t.Fatalf("lastPong = %v; want 2s after createdAt", got)
	}
}

// TestInfoRTTNilUntilFirstPong: a link with no pong reports no RTT; after one it reports the RTT and how long ago it came.
func TestInfoRTTNilUntilFirstPong(t *testing.T) {
	link, _ := testPipeLink(t)

	info := link.Info()
	if info.RTT != nil || info.RTTAge != nil {
		t.Fatalf("RTT, RTTAge before any pong = %v, %v; want nil, nil", info.RTT, info.RTTAge)
	}
	if age := time.Duration(info.Age); age < 0 || age > time.Minute {
		t.Fatalf("Age = %v; want the time since newLink", age)
	}

	link.recordRTT(18*time.Millisecond, time.Now())
	info = link.Info()
	if info.RTT == nil || time.Duration(*info.RTT) != 18*time.Millisecond {
		t.Fatalf("RTT = %v; want 18ms", info.RTT)
	}
	if info.RTTAge == nil || *info.RTTAge < 0 || time.Duration(*info.RTTAge) > time.Minute {
		t.Fatalf("RTTAge = %v; want the time since the pong", info.RTTAge)
	}
	if *info.RTTAge > info.Age {
		t.Fatalf("RTTAge %v exceeds Age %v", *info.RTTAge, info.Age)
	}
}
