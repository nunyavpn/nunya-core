package main

import (
	"ThroneCore/gen"
	"ThroneCore/internal/boxdns"
	"context"
	"errors"
)

func (s *server) SetSystemDNS(ctx context.Context, in *gen.SetSystemDNSRequest) (*gen.EmptyResp, error) {
	err := boxdns.DnsManagerInstance.SetSystemDNS(nil, *in.Clear)
	if err != nil {
		return nil, err
	}

	return &gen.EmptyResp{}, nil
}

// GetDefaultInterface reports the physical default-route interface as tracked by
// the always-on boxdns monitor. The monitor excludes virtual (TUN) and loopback
// interfaces by type, so the result is safe to bind a core egress to even while
// throne-tun is up. Used at config-build time to bake sockopt.interface onto the
// Xray egress and avoid the egress SOCKS loopback bridge.
func (s *server) GetDefaultInterface(ctx context.Context, in *gen.EmptyReq) (*gen.GetDefaultInterfaceResponse, error) {
	if boxdns.DnsManagerInstance == nil || boxdns.DnsManagerInstance.Monitor == nil {
		return nil, errors.New("interface monitor not available")
	}
	ifc := boxdns.DnsManagerInstance.Monitor.DefaultInterface()
	if ifc == nil {
		return nil, errors.New("no default interface")
	}
	return &gen.GetDefaultInterfaceResponse{
		Name:  To(ifc.Name),
		Index: To(int32(ifc.Index)),
	}, nil
}
