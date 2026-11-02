package dto

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
