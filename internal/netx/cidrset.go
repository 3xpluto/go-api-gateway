package netx

import (
	"fmt"
	"net"
	"strings"
)

type CIDRSet struct {
	nets []*net.IPNet
}

func ParseCIDRSet(items []string) (*CIDRSet, error) {
	set := &CIDRSet{}
	for _, raw := range items {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		// Allow plain IP shorthand
		if !strings.Contains(s, "/") {
			ip := net.ParseIP(s)
			if ip == nil {
				return nil, fmt.Errorf("invalid ip: %q", s)
			}
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			s = fmt.Sprintf("%s/%d", ip.String(), bits)
		}
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("invalid cidr %q: %w", s, err)
		}
		set.nets = append(set.nets, n)
	}
	return set, nil
}

func (s *CIDRSet) Contains(ip net.IP) bool {
	if s == nil || len(s.nets) == 0 || ip == nil {
		return false
	}
	for _, n := range s.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
