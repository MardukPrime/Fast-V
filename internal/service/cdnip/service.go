// Package cdnip exposes Cloudflare edge scans to the frontend and persists the
// edge the user picks for a node.
package cdnip

import (
	"context"
	"errors"
	"net/netip"
	"strings"

	cdn "github.com/MardukPrime/Fast-V/internal/cdnip"
	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

// Service is the Wails-facing wrapper around the scanner.
type Service struct {
	store *store.Store
}

// New builds the service on the shared store.
func New(st *store.Store) *Service {
	return &Service{store: st}
}

// ServiceName is the name the frontend bindings use.
func (s *Service) ServiceName() string {
	return "CdnService"
}

// CandidateCount reports how many addresses a scan of this size would test, so
// the UI can show the cost before the user starts one.
func (s *Service) CandidateCount(sample int) int {
	return len(cdn.Candidates(sample, ""))
}

// Scan tests candidate edges for a node (or for an explicit host) and returns the
// fastest ones that actually serve it.
func (s *Service) Scan(input model.CdnScanRequest) (model.CdnScanResult, error) {
	host := strings.TrimSpace(input.Host)
	port := input.Port
	if input.NodeID != "" {
		node, err := s.store.FindNode(input.NodeID)
		if err != nil {
			return model.CdnScanResult{}, err
		}
		host = node.Address
		if port <= 0 {
			port = node.Port
		}
	}
	if host == "" {
		return model.CdnScanResult{}, errors.New("该节点没有域名，CDN 优选需要节点域名作为 Host")
	}
	if port <= 0 {
		port = 443
	}
	input.Host = host
	input.Port = port
	return cdn.Scan(context.Background(), input)
}

// Apply pins the chosen edge on the node. The node's own address is kept, so
// clearing the selection later restores the original behaviour.
func (s *Service) Apply(nodeID string, ip string) (model.NodeView, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return model.NodeView{}, errors.New("无效的 IP 地址")
	}
	updated, err := s.store.SetNodeCdnIP(nodeID, addr.String())
	if err != nil {
		return model.NodeView{}, err
	}
	return s.view(updated), nil
}

// Clear removes the pinned edge and lets the node use its own address again.
func (s *Service) Clear(nodeID string) (model.NodeView, error) {
	updated, err := s.store.SetNodeCdnIP(nodeID, "")
	if err != nil {
		return model.NodeView{}, err
	}
	return s.view(updated), nil
}

func (s *Service) view(item model.Node) model.NodeView {
	subName := ""
	if sub, err := s.store.FindSubscription(item.SubscriptionID); err == nil {
		subName = sub.Name
	}
	return model.NodeViewFrom(item, subName, s.store.Snapshot().ActiveNodeID)
}
