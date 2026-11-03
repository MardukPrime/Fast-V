package subscription

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
	subdomain "github.com/MardukPrime/Fast-V/internal/subscription"
)

// Service manages remote subscription sources and their parsed nodes.
type Service struct {
	store *store.Store
}

func New(store *store.Store) *Service {
	return &Service{store: store}
}

func (s *Service) ServiceName() string {
	return "SubscriptionService"
}

func (s *Service) List() []model.SubscriptionView {
	subs := s.store.ListSubscriptions()
	out := make([]model.SubscriptionView, 0, len(subs))
	for _, sub := range subs {
		out = append(out, subscriptionView(sub))
	}
	return out
}

func (s *Service) Add(input model.SubscriptionInput) (model.SubscriptionView, error) {
	rawURL := strings.TrimSpace(input.URL)
	if rawURL == "" {
		return model.SubscriptionView{}, errors.New("subscription URL is required")
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil || parsedURL.Hostname() == "" {
		return model.SubscriptionView{}, errors.New("invalid subscription URL")
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return model.SubscriptionView{}, errors.New("subscription URL must use http or https")
	}

	content, err := subdomain.Fetch(context.Background(), rawURL)
	if err != nil {
		return model.SubscriptionView{}, err
	}
	parsedNodes, err := subdomain.ParseContent(content)
	if err != nil {
		return model.SubscriptionView{}, err
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = parsedURL.Hostname()
	}

	now := model.NowRFC3339()
	sub := model.Subscription{
		ID:        model.NewID(),
		Name:      name,
		URL:       rawURL,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	nodes := subdomain.BuildNodes(sub.ID, parsedNodes)
	if err := s.store.AddSubscription(sub, nodes); err != nil {
		return model.SubscriptionView{}, err
	}

	sub.NodeCount = len(nodes)
	return subscriptionView(sub), nil
}

func (s *Service) Update(id string) (model.SubscriptionView, error) {
	sub, err := s.store.FindSubscription(id)
	if err != nil {
		return model.SubscriptionView{}, err
	}

	content, fetchErr := subdomain.Fetch(context.Background(), sub.URL)
	if fetchErr != nil {
		_ = s.store.SetSubscriptionError(id, fetchErr.Error())
		sub.LastError = fetchErr.Error()
		return subscriptionView(sub), fetchErr
	}
	parsedNodes, parseErr := subdomain.ParseContent(content)
	if parseErr != nil {
		_ = s.store.SetSubscriptionError(id, parseErr.Error())
		sub.LastError = parseErr.Error()
		return subscriptionView(sub), parseErr
	}

	nodes := subdomain.BuildNodes(id, parsedNodes)
	if err := s.store.ReplaceSubscriptionNodes(id, nodes); err != nil {
		return model.SubscriptionView{}, err
	}

	sub.NodeCount = len(nodes)
	sub.LastError = ""
	sub.UpdatedAt = model.NowRFC3339()
	return subscriptionView(sub), nil
}

func (s *Service) UpdateAll() (model.UpdateAllResult, error) {
	subs := s.store.ListSubscriptions()
	result := model.UpdateAllResult{
		Subscriptions: make([]model.SubscriptionView, 0, len(subs)),
		Errors:        make([]string, 0),
	}

	for _, sub := range subs {
		if !sub.Enabled {
			continue
		}
		updated, err := s.Update(sub.ID)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", sub.Name, err))
			result.Subscriptions = append(result.Subscriptions, updated)
			continue
		}
		result.Updated++
		result.Subscriptions = append(result.Subscriptions, updated)
	}
	return result, nil
}

func (s *Service) Delete(id string) error {
	return s.store.DeleteSubscription(id)
}

func (s *Service) Nodes(id string) ([]model.NodeView, error) {
	sub, err := s.store.FindSubscription(id)
	if err != nil {
		return nil, err
	}
	activeID := s.store.Snapshot().ActiveNodeID
	nodes := s.store.NodesForSubscription(id)
	out := make([]model.NodeView, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, model.NodeViewFrom(node, sub.Name, activeID))
	}
	return out, nil
}

func subscriptionView(sub model.Subscription) model.SubscriptionView {
	view := model.SubscriptionViewFrom(sub)
	if parsed, err := url.Parse(sub.URL); err == nil {
		view.Host = parsed.Hostname()
	}
	return view
}
