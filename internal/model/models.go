package model

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// AppInfo describes the running FastV application.
type AppInfo struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	Commit       string `json:"commit"`
	BuildDate    string `json:"buildDate"`
	Platform     string `json:"platform"`
	Arch         string `json:"arch"`
	WailsVersion string `json:"wailsVersion"`
}

// Settings contains the basic application settings.
type Settings struct {
	Theme                 string `json:"theme"`
	Language              string `json:"language"`
	AutoUpdate            bool   `json:"autoUpdate"`
	UpdateIntervalMinutes int    `json:"updateIntervalMinutes"`
	ProxyMode             string `json:"proxyMode"`
	CoreAutoUpdate        bool   `json:"coreAutoUpdate"`
	CoreType              string `json:"coreType"`
	XrayLocalPort         int    `json:"xrayLocalPort"`
	XrayLogLevel          string `json:"xrayLogLevel"`
	XrayMuxEnabled        bool   `json:"xrayMuxEnabled"`
	XrayMuxConcurrency    int    `json:"xrayMuxConcurrency"`
	XraySniffing          bool   `json:"xraySniffing"`
}

// Subscription is the internal subscription record. URL contains a token and is never exposed directly.
type Subscription struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	LastError string `json:"lastError"`
	NodeCount int    `json:"nodeCount"`
}

// SubscriptionView is the frontend-safe subscription representation.
type SubscriptionView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Host      string `json:"host"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	LastError string `json:"lastError"`
	NodeCount int    `json:"nodeCount"`
}

// SubscriptionInput is the payload for adding a subscription.
type SubscriptionInput struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Node is the internal parsed node record. Raw contains credentials and is never exposed directly.
type Node struct {
	ID             string `json:"id"`
	SubscriptionID string `json:"subscriptionId"`
	Name           string `json:"name"`
	Protocol       string `json:"protocol"`
	Address        string `json:"address"`
	Port           int    `json:"port"`
	Raw            string `json:"raw"`
	Enabled        bool   `json:"enabled"`
	SourceKey      string `json:"sourceKey"`
}

// NodeView is the frontend-safe node representation.
type NodeView struct {
	ID               string `json:"id"`
	SubscriptionID   string `json:"subscriptionId"`
	SubscriptionName string `json:"subscriptionName"`
	Name             string `json:"name"`
	Protocol         string `json:"protocol"`
	Address          string `json:"address"`
	Port             int    `json:"port"`
	Enabled          bool   `json:"enabled"`
	Active           bool   `json:"active"`
}

// DashboardSummary is the data shown on the overview page.
type DashboardSummary struct {
	App                 AppInfo            `json:"app"`
	SubscriptionCount   int                `json:"subscriptionCount"`
	NodeCount           int                `json:"nodeCount"`
	ActiveSubscriptions int                `json:"activeSubscriptions"`
	LastUpdatedAt       string             `json:"lastUpdatedAt"`
	CoreStatus          string             `json:"coreStatus"`
	CoreVersion         string             `json:"coreVersion"`
	Settings            Settings           `json:"settings"`
	RecentSubscriptions []SubscriptionView `json:"recentSubscriptions"`
	ActiveNode          *NodeView          `json:"activeNode"`
}

// UpdateAllResult summarizes a bulk subscription refresh.
type UpdateAllResult struct {
	Updated       int                `json:"updated"`
	Failed        int                `json:"failed"`
	Subscriptions []SubscriptionView `json:"subscriptions"`
	Errors        []string           `json:"errors"`
}

// CoreInfo describes the locally installed Xray-core and the latest known release.
type CoreInfo struct {
	Installed     bool   `json:"installed"`
	Version       string `json:"version"`
	LatestVersion string `json:"latestVersion"`
	AssetName     string `json:"assetName"`
	Path          string `json:"path"`
	Status        string `json:"status"`
	Message       string `json:"message"`
	AutoUpdate    bool   `json:"autoUpdate"`
	CheckedAt     string `json:"checkedAt"`
}

// CoreUpdateResult summarizes an automatic or manual update operation.
type CoreUpdateResult struct {
	Updated bool     `json:"updated"`
	Info    CoreInfo `json:"info"`
	Message string   `json:"message"`
}

// NodeFilter controls the node management list.
type NodeFilter struct {
	Search         string `json:"search"`
	Protocol       string `json:"protocol"`
	SubscriptionID string `json:"subscriptionId"`
}

// NodeLatency is the result of a direct TCP reachability check.
type NodeLatency struct {
	NodeID    string `json:"nodeId"`
	LatencyMS int64  `json:"latencyMs"`
	Error     string `json:"error"`
	CheckedAt string `json:"checkedAt"`
}

// SourceKey returns a stable hash used to preserve node identity across subscription updates.
func SourceKey(value string) string {
	if value == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

// SubscriptionViewFrom converts an internal subscription to a frontend-safe view.
func SubscriptionViewFrom(sub Subscription) SubscriptionView {
	return SubscriptionView{
		ID:        sub.ID,
		Name:      sub.Name,
		Enabled:   sub.Enabled,
		CreatedAt: sub.CreatedAt,
		UpdatedAt: sub.UpdatedAt,
		LastError: sub.LastError,
		NodeCount: sub.NodeCount,
	}
}

// NodeViewFrom converts an internal node to a frontend-safe view.
func NodeViewFrom(node Node, subscriptionName string, activeID string) NodeView {
	return NodeView{
		ID:               node.ID,
		SubscriptionID:   node.SubscriptionID,
		SubscriptionName: subscriptionName,
		Name:             node.Name,
		Protocol:         node.Protocol,
		Address:          node.Address,
		Port:             node.Port,
		Enabled:          node.Enabled,
		Active:           node.ID == activeID,
	}
}

// NewID returns a short random identifier for local records.
func NewID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return time.Now().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(buf)
}

// NowRFC3339 returns the current time in the format used by local records.
func NowRFC3339() string {
	return time.Now().Format(time.RFC3339)
}
