package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SubscriptionService manages remote subscription sources and their parsed nodes.
type SubscriptionService struct {
	store *Store
}

func NewSubscriptionService(store *Store) *SubscriptionService {
	return &SubscriptionService{store: store}
}

func (s *SubscriptionService) ServiceName() string {
	return "SubscriptionService"
}

func (s *SubscriptionService) List() []SubscriptionView {
	subs := s.store.ListSubscriptions()
	out := make([]SubscriptionView, 0, len(subs))
	for _, sub := range subs {
		out = append(out, toSubscriptionView(sub))
	}
	return out
}

func (s *SubscriptionService) Add(input SubscriptionInput) (SubscriptionView, error) {
	rawURL := strings.TrimSpace(input.URL)
	if rawURL == "" {
		return SubscriptionView{}, errors.New("subscription URL is required")
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil || parsedURL.Hostname() == "" {
		return SubscriptionView{}, errors.New("invalid subscription URL")
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return SubscriptionView{}, errors.New("subscription URL must use http or https")
	}

	content, err := fetchSubscription(rawURL)
	if err != nil {
		return SubscriptionView{}, err
	}
	parsedNodes, err := parseSubscriptionContent(content)
	if err != nil {
		return SubscriptionView{}, err
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = parsedURL.Hostname()
	}

	now := nowRFC3339()
	sub := Subscription{
		ID:        newID(),
		Name:      name,
		URL:       rawURL,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	nodes := buildNodes(sub.ID, parsedNodes)
	if err := s.store.AddSubscription(sub, nodes); err != nil {
		return SubscriptionView{}, err
	}

	sub.NodeCount = len(nodes)
	return toSubscriptionView(sub), nil
}

func (s *SubscriptionService) Update(id string) (SubscriptionView, error) {
	sub, err := s.store.FindSubscription(id)
	if err != nil {
		return SubscriptionView{}, err
	}

	content, fetchErr := fetchSubscription(sub.URL)
	if fetchErr != nil {
		_ = s.store.SetSubscriptionError(id, fetchErr.Error())
		sub.LastError = fetchErr.Error()
		return toSubscriptionView(sub), fetchErr
	}
	parsedNodes, parseErr := parseSubscriptionContent(content)
	if parseErr != nil {
		_ = s.store.SetSubscriptionError(id, parseErr.Error())
		sub.LastError = parseErr.Error()
		return toSubscriptionView(sub), parseErr
	}

	nodes := buildNodes(id, parsedNodes)
	if err := s.store.ReplaceSubscriptionNodes(id, nodes); err != nil {
		return SubscriptionView{}, err
	}

	sub.NodeCount = len(nodes)
	sub.LastError = ""
	sub.UpdatedAt = nowRFC3339()
	return toSubscriptionView(sub), nil
}

func (s *SubscriptionService) UpdateAll() (UpdateAllResult, error) {
	subs := s.store.ListSubscriptions()
	result := UpdateAllResult{
		Subscriptions: make([]SubscriptionView, 0, len(subs)),
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

func (s *SubscriptionService) Delete(id string) error {
	return s.store.DeleteSubscription(id)
}

func (s *SubscriptionService) Nodes(id string) ([]NodeView, error) {
	if _, err := s.store.FindSubscription(id); err != nil {
		return nil, err
	}
	nodes := s.store.NodesForSubscription(id)
	out := make([]NodeView, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, toNodeView(node))
	}
	return out, nil
}

func fetchSubscription(rawURL string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "FastV/0.1.0")
	req.Header.Set("Accept", "*/*")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch subscription: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("subscription returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return "", fmt.Errorf("failed to read subscription: %w", err)
	}
	if len(body) == 0 {
		return "", errors.New("subscription response is empty")
	}
	return string(body), nil
}

func buildNodes(subscriptionID string, parsed []parsedNode) []Node {
	nodes := make([]Node, 0, len(parsed))
	for _, item := range parsed {
		nodes = append(nodes, Node{
			ID:             newID(),
			SubscriptionID: subscriptionID,
			Name:           item.Name,
			Protocol:       item.Protocol,
			Address:        item.Address,
			Port:           item.Port,
			Raw:            item.Raw,
		})
	}
	return nodes
}

func toSubscriptionView(sub Subscription) SubscriptionView {
	host := ""
	if parsed, err := url.Parse(sub.URL); err == nil {
		host = parsed.Hostname()
	}
	return SubscriptionView{
		ID:        sub.ID,
		Name:      sub.Name,
		Host:      host,
		Enabled:   sub.Enabled,
		CreatedAt: sub.CreatedAt,
		UpdatedAt: sub.UpdatedAt,
		LastError: sub.LastError,
		NodeCount: sub.NodeCount,
	}
}

func toNodeView(node Node) NodeView {
	return NodeView{
		ID:             node.ID,
		SubscriptionID: node.SubscriptionID,
		Name:           node.Name,
		Protocol:       node.Protocol,
		Address:        node.Address,
		Port:           node.Port,
	}
}
