package relay

import "testing"

func TestDirectReplyPortOffsetsByTenThousand(t *testing.T) {
	if got := directReplyPort(40000); got != 50000 {
		t.Fatalf("directReplyPort(40000) = %d, want 50000", got)
	}
	if got := directReplyPort(40417); got != 50417 {
		t.Fatalf("directReplyPort(40417) = %d, want 50417", got)
	}
}
