package main

import "sort"

// DashboardService provides aggregate data for the overview page.
type DashboardService struct {
	store *Store
}

func NewDashboardService(store *Store) *DashboardService {
	return &DashboardService{store: store}
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

	return DashboardSummary{
		App:                 NewAppService().Info(),
		SubscriptionCount:   len(snapshot.Subscriptions),
		NodeCount:           len(snapshot.Nodes),
		ActiveSubscriptions: active,
		LastUpdatedAt:       lastUpdated,
		CoreStatus:          "not_configured",
		CoreVersion:         "not connected",
		Settings:            snapshot.Settings,
		RecentSubscriptions: recentViews,
	}
}
