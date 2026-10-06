package xraydns

import (
	"context"
	"errors"
	"testing"

	"github.com/nunyavpn/nunya-core/internal/boxbox"
	"github.com/nunyavpn/nunya-core/internal/boxmain"

	C "github.com/sagernet/sing-box/constant"
	dnsfeature "github.com/xtls/xray-core/features/dns"
)

// A rule sends every query to dns-remote, so a dns-direct answer proves the pin beat the rule chain.
const splitAnswerConfig = `{
  "log": {"disabled": true},
  "dns": {
    "servers": [
      {"type": "hosts", "tag": "dns-direct", "predefined": {"pinned.invalid": ["10.0.0.1"]}},
      {"type": "hosts", "tag": "dns-remote", "predefined": {"pinned.invalid": ["10.0.0.2"]}}
    ],
    "rules": [{"domain": "pinned.invalid", "action": "route", "server": "dns-remote"}],
    "final": "dns-remote"
  },
  "outbounds": [{"type": "direct", "tag": "direct"}]
}`

// Same, minus dns-direct: what a user-supplied raw DNS object can look like.
const noDirectConfig = `{
  "log": {"disabled": true},
  "dns": {
    "servers": [
      {"type": "hosts", "tag": "dns-remote", "predefined": {"pinned.invalid": ["10.0.0.2"]}}
    ],
    "final": "dns-remote"
  },
  "outbounds": [{"type": "direct", "tag": "direct"}]
}`

func startBox(t *testing.T, config string) *boxbox.Box {
	t.Helper()
	box, cancel, err := boxmain.Create([]byte(config), nil)
	if err != nil {
		t.Fatalf("create box: %v", err)
	}
	t.Cleanup(func() {
		_ = box.Close()
		cancel()
	})
	return box
}

func lookupV4(t *testing.T, box *boxbox.Box, domain string) []string {
	t.Helper()
	resolver := New(func() context.Context { return box.Context() })
	ips, _, err := resolver.LookupIP(domain, dnsfeature.IPOption{IPv4Enable: true})
	if err != nil {
		t.Fatalf("lookup %s: %v", domain, err)
	}
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out
}

func TestLookupPinsDNSDirectOverRules(t *testing.T) {
	got := lookupV4(t, startBox(t, splitAnswerConfig), "pinned.invalid")
	if len(got) != 1 || got[0] != "10.0.0.1" {
		t.Fatalf("expected the dns-direct answer 10.0.0.1, got %v", got)
	}
}

func TestLookupFallsBackToRulesWithoutDNSDirect(t *testing.T) {
	got := lookupV4(t, startBox(t, noDirectConfig), "pinned.invalid")
	if len(got) != 1 || got[0] != "10.0.0.2" {
		t.Fatalf("expected the rule-selected answer 10.0.0.2, got %v", got)
	}
}

// A lookup can legitimately arrive before the box exists; it must report, not panic.
func TestLookupBeforeBoxExists(t *testing.T) {
	resolver := New(func() context.Context { return nil })
	if _, _, err := resolver.LookupIP("pinned.invalid", dnsfeature.IPOption{IPv4Enable: true}); err == nil {
		t.Fatal("expected an error while no box is up")
	}
}

// LookupForIPWithClient asks for neither family when the strategy and the bound local address disagree.
func TestLookupWithNoFamilyEnabled(t *testing.T) {
	resolver := New(func() context.Context { return startBox(t, splitAnswerConfig).Context() })
	_, _, err := resolver.LookupIP("pinned.invalid", dnsfeature.IPOption{})
	if !errors.Is(err, dnsfeature.ErrEmptyResponse) {
		t.Fatalf("expected ErrEmptyResponse, got %v", err)
	}
}

func TestStrategyFor(t *testing.T) {
	for _, tc := range []struct {
		name   string
		option dnsfeature.IPOption
		want   C.DomainStrategy
	}{
		{"both takes the default resolver's strategy", dnsfeature.IPOption{IPv4Enable: true, IPv6Enable: true}, C.DomainStrategyPreferIPv4},
		{"v4 only", dnsfeature.IPOption{IPv4Enable: true}, C.DomainStrategyIPv4Only},
		{"v6 only", dnsfeature.IPOption{IPv6Enable: true}, C.DomainStrategyIPv6Only},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := strategyFor(tc.option, C.DomainStrategyPreferIPv4); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// What the client sends: apps are told IPv4 only (a TUN without IPv6), while the server's own
// address is asked for both families through route.default_domain_resolver.
const v6OnlyServerConfig = `{
  "log": {"disabled": true},
  "dns": {
    "servers": [
      {"type": "hosts", "tag": "dns-direct", "predefined": {"v6only.invalid": ["2001:db8::1"]}}
    ],
    "final": "dns-direct",
    "strategy": "ipv4_only"
  },
  "route": {"default_domain_resolver": {"server": "dns-direct", "strategy": "prefer_ipv4"}},
  "outbounds": [{"type": "direct", "tag": "direct"}]
}`

// The same without a strategy of the resolver's own: the DNS block's ipv4_only applies, as before.
const v6OnlyNoResolverStrategyConfig = `{
  "log": {"disabled": true},
  "dns": {
    "servers": [
      {"type": "hosts", "tag": "dns-direct", "predefined": {"v6only.invalid": ["2001:db8::1"]}}
    ],
    "final": "dns-direct",
    "strategy": "ipv4_only"
  },
  "route": {"default_domain_resolver": "dns-direct"},
  "outbounds": [{"type": "direct", "tag": "direct"}]
}`

func lookupBoth(box *boxbox.Box, domain string) ([]string, error) {
	resolver := New(func() context.Context { return box.Context() })
	ips, _, err := resolver.LookupIP(domain, dnsfeature.IPOption{IPv4Enable: true, IPv6Enable: true})
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out, err
}

func TestAnIPv6OnlyServerResolvesUnderTheDefaultResolversStrategy(t *testing.T) {
	got, err := lookupBoth(startBox(t, v6OnlyServerConfig), "v6only.invalid")
	if err != nil || len(got) != 1 || got[0] != "2001:db8::1" {
		t.Fatalf("expected 2001:db8::1 under prefer_ipv4, got %v (%v)", got, err)
	}
}

func TestWithoutAResolverStrategyTheDNSBlocksStillApplies(t *testing.T) {
	got, err := lookupBoth(startBox(t, v6OnlyNoResolverStrategyConfig), "v6only.invalid")
	if err == nil && len(got) > 0 {
		t.Fatalf("expected no answer under the DNS block's ipv4_only, got %v", got)
	}
}
