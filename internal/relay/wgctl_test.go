package relay

import "testing"

func TestDirectReplyPortOffsetsByOneThousand(t *testing.T) {
	if got := directReplyPort(40000); got != 41000 {
		t.Fatalf("directReplyPort(40000) = %d, want 41000", got)
	}
	if got := directReplyPort(40417); got != 41417 {
		t.Fatalf("directReplyPort(40417) = %d, want 41417", got)
	}
}
