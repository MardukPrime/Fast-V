package dashboard

import (
	"sort"

	"github.com/MardukPrime/Fast-V/internal/model"
	appservice "github.com/MardukPrime/Fast-V/internal/service/app"
	"github.com/MardukPrime/Fast-V/internal/store"
)

type CoreInfoProvider interface {
	Info() model.CoreInfo
}

// DashboardService provides aggregate data for the overview page.
type DashboardService struct {
	store *store.Store
	core  CoreInfoProvider
}

func New(store *store.Store, core CoreInfoProvider) *DashboardService {
	return &DashboardService{store: store, core: core}
}
func (s *DashboardService) ServiceName() string {
	return "DashboardService"
}

func (s *DashboardService) Summary() model.DashboardSummary {
	snapshot := s.store.Snapshot()
	active := 0
	lastUpdated := ""
	var uploadBytes, downloadBytes, totalBytes int64
	for _, sub := range snapshot.Subscriptions {
		if sub.Enabled {
			uploadBytes += sub.UploadBytes
			downloadBytes += sub.DownloadBytes
			totalBytes += sub.TotalBytes
			active++
		}
		if sub.UpdatedAt > lastUpdated {
			lastUpdated = sub.UpdatedAt
		}
	}

	recent := append([]model.Subscription(nil), snapshot.Subscriptions...)
	sort.Slice(recent, func(i, j int) bool {
		return recent[i].UpdatedAt > recent[j].UpdatedAt
	})
	if len(recent) > 5 {
		recent = recent[:5]
	}
	recentViews := make([]model.SubscriptionView, 0, len(recent))
	for _, sub := range recent {
		recentViews = append(recentViews, model.SubscriptionViewFrom(sub))
	}

	coreInfo := s.core.Info()
	coreVersion := coreInfo.Version
	if coreVersion == "" {
		coreVersion = "未安装"
	}

	var activeNode *model.NodeView
	if node, ok := s.store.ActiveNode(); ok {
		subscriptionName := ""
		if sub, err := s.store.FindSubscription(node.SubscriptionID); err == nil {
			subscriptionName = sub.Name
		}
		view := model.NodeViewFrom(node, subscriptionName, node.ID)
		activeNode = &view
	}

	return model.DashboardSummary{
		App:                 appservice.New().Info(),
		SubscriptionCount:   len(snapshot.Subscriptions),
		NodeCount:           len(snapshot.Nodes),
		ActiveSubscriptions: active,
		LastUpdatedAt:       lastUpdated,
		CoreStatus:          coreInfo.Status,
		CoreVersion:         coreVersion,
		Settings:            snapshot.Settings,
		RecentSubscriptions: recentViews,
		ActiveNode:          activeNode,
		UploadBytes:         uploadBytes,
		DownloadBytes:       downloadBytes,
		TotalBytes:          totalBytes,
	}
}
