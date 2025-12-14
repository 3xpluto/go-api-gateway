package netx

import (
	"net"
	"testing"
)

func TestCIDRSetContains(t *testing.T) {
	set, err := ParseCIDRSet([]string{"10.0.0.0/8", "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	if !set.Contains(net.ParseIP("10.1.2.3")) {
		t.Fatal("expected 10.1.2.3 to be contained")
	}
	if !set.Contains(net.ParseIP("127.0.0.1")) {
		t.Fatal("expected 127.0.0.1 to be contained")
	}
	if set.Contains(net.ParseIP("192.168.1.1")) {
		t.Fatal("did not expect 192.168.1.1 to be contained")
	}
}
