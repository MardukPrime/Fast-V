package subscription

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/skip2/go-qrcode"
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
	subdomain "github.com/MardukPrime/Fast-V/internal/subscription"
)

// Service manages remote subscription sources and their parsed nodes.
type Service struct {
	store  *store.Store
	mu     sync.Mutex
	cancel context.CancelFunc
}

func New(store *store.Store) *Service {
	return &Service{store: store}
}

func (s *Service) ServiceName() string {
	return "SubscriptionService"
}

func (s *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	serviceCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	go s.autoUpdateLoop(serviceCtx)
	return nil
}

func (s *Service) ServiceShutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	return nil
}

// Subscription scheduling and option validation limits.
const (
	// minSubscriptionUpdateIntervalMinutes is the smallest per-subscription
	// refresh interval accepted by SetOptions. Zero means "follow the global
	// Settings.UpdateIntervalMinutes".
	minSubscriptionUpdateIntervalMinutes = 15
	// maxSubscriptionUpdateIntervalMinutes is the largest accepted refresh interval.
	maxSubscriptionUpdateIntervalMinutes = 1440
	// defaultSubscriptionUpdateIntervalMinutes guards against a corrupt global
	// interval value and matches the Settings default of 360 minutes.
	defaultSubscriptionUpdateIntervalMinutes = 360
	// maxSubscriptionUserAgentLength caps the custom User-Agent header length.
	maxSubscriptionUserAgentLength = 200

	// autoUpdateInitialDelay lets the app finish starting before the first
	// automatic refresh check runs.
	autoUpdateInitialDelay = 5 * time.Second
	// autoUpdatePollInterval is how often due subscriptions are evaluated.
	autoUpdatePollInterval = time.Minute
)

// autoUpdateLoop refreshes subscriptions on their own schedules. The global
// Settings.AutoUpdate switch is a master switch: while it is off nothing is
// refreshed automatically.
func (s *Service) autoUpdateLoop(ctx context.Context) {
	timer := time.NewTimer(autoUpdateInitialDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		s.runAutoUpdate(time.Now())
		timer.Reset(autoUpdatePollInterval)
	}
}

// runAutoUpdate performs one scheduling tick: every subscription whose own
// interval has elapsed is refreshed and the UI is notified. Update records
// fetch and parse errors on the subscription itself, so one failing endpoint
// never blocks the others.
func (s *Service) runAutoUpdate(now time.Time) {
	due := dueSubscriptions(s.store.ListSubscriptions(), s.store.Settings(), now)
	if len(due) == 0 {
		return
	}
	for _, id := range due {
		_, _ = s.Update(id)
	}
	if app := application.Get(); app != nil {
		app.Event.Emit("subscriptions:changed")
	}
}

// dueSubscriptions returns the IDs of the subscriptions that are due for an
// automatic refresh at now. Rules:
//   - Settings.AutoUpdate is the global master switch; while it is off nothing is due;
//   - only Enabled subscriptions with their own AutoUpdate switch on are considered;
//   - the effective interval is the subscription's UpdateIntervalMinutes, or the
//     global Settings.UpdateIntervalMinutes when the subscription value is zero;
//   - a subscription is due when UpdatedAt is empty (never refreshed) or when at
//     least the effective interval has elapsed since UpdatedAt.
func dueSubscriptions(subs []model.Subscription, settings model.Settings, now time.Time) []string {
	due := make([]string, 0, len(subs))
	if !settings.AutoUpdate {
		return due
	}
	globalInterval := settings.UpdateIntervalMinutes
	if globalInterval <= 0 {
		globalInterval = defaultSubscriptionUpdateIntervalMinutes
	}
	for _, sub := range subs {
		if !sub.Enabled || !sub.AutoUpdate {
			continue
		}
		interval := sub.UpdateIntervalMinutes
		if interval <= 0 {
			interval = globalInterval
		}
		if interval < minSubscriptionUpdateIntervalMinutes {
			interval = minSubscriptionUpdateIntervalMinutes
		}
		if sub.UpdatedAt == "" {
			due = append(due, sub.ID)
			continue
		}
		updatedAt, err := time.Parse(time.RFC3339, sub.UpdatedAt)
		if err != nil {
			// A hand-edited or truncated timestamp must not stop refreshes forever.
			due = append(due, sub.ID)
			continue
		}
		if now.Sub(updatedAt) >= time.Duration(interval)*time.Minute {
			due = append(due, sub.ID)
		}
	}
	return due
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

	fetched, err := subdomain.FetchWithMeta(context.Background(), rawURL)
	if err != nil {
		return model.SubscriptionView{}, err
	}
	parsedNodes, err := subdomain.ParseContent(fetched.Content)
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
		// New subscriptions refresh automatically with the global schedule until
		// the user customises the options for this subscription.
		AutoUpdate:            true,
		UpdateIntervalMinutes: 0,
		UserAgent:             "",
	}
	nodes := subdomain.BuildNodes(sub.ID, parsedNodes)
	if err := s.store.AddSubscription(sub, nodes); err != nil {
		return model.SubscriptionView{}, err
	}
	if fetched.UserInfo.Present {
		applyUserInfo(&sub, fetched.UserInfo)
		_ = s.store.SetSubscriptionUsage(sub.ID, sub.UploadBytes, sub.DownloadBytes, sub.TotalBytes, sub.ExpireAt, sub.UsageUpdatedAt)
	}

	sub.NodeCount = len(nodes)
	return subscriptionView(sub), nil
}

// SetOptions updates the per-subscription automatic update switch, the optional
// refresh interval and the custom User-Agent header. intervalMinutes zero means
// "follow the global Settings.UpdateIntervalMinutes".
func (s *Service) SetOptions(id string, autoUpdate bool, intervalMinutes int, userAgent string) (model.SubscriptionView, error) {
	id = strings.TrimSpace(id)
	if intervalMinutes != 0 && (intervalMinutes < minSubscriptionUpdateIntervalMinutes || intervalMinutes > maxSubscriptionUpdateIntervalMinutes) {
		return model.SubscriptionView{}, fmt.Errorf("更新间隔必须在 %d 到 %d 分钟之间", minSubscriptionUpdateIntervalMinutes, maxSubscriptionUpdateIntervalMinutes)
	}
	if strings.ContainsAny(userAgent, "\r\n") {
		return model.SubscriptionView{}, errors.New("User-Agent 不能包含换行符")
	}
	trimmedUserAgent := strings.TrimSpace(userAgent)
	if utf8.RuneCountInString(trimmedUserAgent) > maxSubscriptionUserAgentLength {
		return model.SubscriptionView{}, fmt.Errorf("User-Agent 长度不能超过 %d 个字符", maxSubscriptionUserAgentLength)
	}
	if _, err := s.store.FindSubscription(id); err != nil {
		return model.SubscriptionView{}, fmt.Errorf("订阅不存在: %s", id)
	}
	stored, err := s.store.SetSubscriptionOptions(id, autoUpdate, intervalMinutes, trimmedUserAgent)
	if err != nil {
		return model.SubscriptionView{}, err
	}
	return subscriptionView(stored), nil
}

func (s *Service) Update(id string) (model.SubscriptionView, error) {
	sub, err := s.store.FindSubscription(id)
	if err != nil {
		return model.SubscriptionView{}, err
	}

	fetched, fetchErr := subdomain.FetchWithMetaOptions(context.Background(), sub.URL, sub.UserAgent)
	if fetchErr != nil {
		_ = s.store.SetSubscriptionError(id, fetchErr.Error())
		sub.LastError = fetchErr.Error()
		return subscriptionView(sub), fetchErr
	}
	parsedNodes, parseErr := subdomain.ParseContent(fetched.Content)
	if parseErr != nil {
		_ = s.store.SetSubscriptionError(id, parseErr.Error())
		sub.LastError = parseErr.Error()
		return subscriptionView(sub), parseErr
	}

	nodes := subdomain.BuildNodes(id, parsedNodes)
	if err := s.store.ReplaceSubscriptionNodes(id, nodes); err != nil {
		return model.SubscriptionView{}, err
	}
	if fetched.UserInfo.Present {
		applyUserInfo(&sub, fetched.UserInfo)
		_ = s.store.SetSubscriptionUsage(id, sub.UploadBytes, sub.DownloadBytes, sub.TotalBytes, sub.ExpireAt, sub.UsageUpdatedAt)
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

// qrCodeImageSize is the edge length in pixels of the generated QR code image.
const qrCodeImageSize = 320

// QRCode renders the subscription URL as a 320px PNG data URL. The URL contains
// the user's private token, so generating the code is an explicit sharing
// action initiated by the user.
func (s *Service) QRCode(id string) (string, error) {
	sub, err := s.store.FindSubscription(id)
	if err != nil {
		return "", fmt.Errorf("订阅不存在: %s", id)
	}
	link := strings.TrimSpace(sub.URL)
	if link == "" {
		return "", errors.New("订阅链接为空，无法生成二维码")
	}
	pngData, err := qrcode.Encode(link, qrcode.Medium, qrCodeImageSize)
	if err != nil {
		return "", fmt.Errorf("订阅 %q 的链接过长，无法生成二维码: %w", sub.Name, err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngData), nil
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

func applyUserInfo(sub *model.Subscription, info subdomain.UserInfo) {
	if !info.Present {
		return
	}
	sub.UploadBytes = info.UploadBytes
	sub.DownloadBytes = info.DownloadBytes
	sub.TotalBytes = info.TotalBytes
	sub.ExpireAt = info.ExpireAt
	sub.UsageUpdatedAt = model.NowRFC3339()
}
func subscriptionView(sub model.Subscription) model.SubscriptionView {
	view := model.SubscriptionViewFrom(sub)
	if parsed, err := url.Parse(sub.URL); err == nil {
		view.Host = parsed.Hostname()
	}
	return view
}
