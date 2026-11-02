package main

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
