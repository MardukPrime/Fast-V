package main

import "sort"

// DashboardService provides aggregate data for the overview page.
type DashboardService struct {
	store *Store
	core  *CoreService
}

func NewDashboardService(store *Store, core *CoreService) *DashboardService {
	return &DashboardService{store: store, core: core}
}

func (s *DashboardService) ServiceName() string {
	return "DashboardService"
}

func (s *DashboardService) Summary() DashboardSummary {
	snapshot := s.store.snapshot()
	active := 0
	lastUpdated := ""
	for _, sub := range snapshot.Subscriptions {
		if sub.Enabled {
			active++
		}
		if sub.UpdatedAt > lastUpdated {
			lastUpdated = sub.UpdatedAt
		}
	}

	recent := append([]Subscription(nil), snapshot.Subscriptions...)
	sort.Slice(recent, func(i, j int) bool {
		return recent[i].UpdatedAt > recent[j].UpdatedAt
	})
	if len(recent) > 5 {
		recent = recent[:5]
	}
	recentViews := make([]SubscriptionView, 0, len(recent))
	for _, sub := range recent {
		recentViews = append(recentViews, toSubscriptionView(sub))
	}

	coreInfo := s.core.Info()
	coreVersion := coreInfo.Version
	if coreVersion == "" {
		coreVersion = "未安装"
	}

	var activeNode *NodeView
	if node, ok := s.store.ActiveNode(); ok {
		subscriptionName := ""
		if sub, err := s.store.FindSubscription(node.SubscriptionID); err == nil {
			subscriptionName = sub.Name
		}
		view := toNodeView(node, subscriptionName, node.ID)
		activeNode = &view
	}

	return DashboardSummary{
		App:                 NewAppService().Info(),
		SubscriptionCount:   len(snapshot.Subscriptions),
		NodeCount:           len(snapshot.Nodes),
		ActiveSubscriptions: active,
		LastUpdatedAt:       lastUpdated,
		CoreStatus:          coreInfo.Status,
		CoreVersion:         coreVersion,
		Settings:            snapshot.Settings,
		RecentSubscriptions: recentViews,
		ActiveNode:          activeNode,
	}
}
