package nat

import "testing"

func TestCandidatePortsBounds(t *testing.T) {
	tests := []struct {
		center, spread int
		first, last    int
	}{
		{1000, 10, 990, 1010},
		{1, 10, 1, 11},
		{65535, 10, 65525, 65535},
		{65530, 10, 65520, 65535},
	}
	for _, tt := range tests {
		ports := candidatePorts(tt.center, tt.spread)
		want := tt.last - tt.first + 1
		if len(ports) != want || ports[0] != tt.first || ports[len(ports)-1] != tt.last {
			t.Errorf("candidatePorts(%d,%d) = len %d [%d..%d], want len %d [%d..%d]",
				tt.center, tt.spread, len(ports), ports[0], ports[len(ports)-1], want, tt.first, tt.last)
		}
	}
}
