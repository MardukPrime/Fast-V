package model

import "testing"

func TestSubscriptionViewFromIncludesUsage(t *testing.T) {
	sub := Subscription{
		ID:             "sub-1",
		Name:           "主订阅",
		Enabled:        true,
		NodeCount:      3,
		LastError:      "",
		UploadBytes:    1024,
		DownloadBytes:  2048,
		TotalBytes:     4096,
		ExpireAt:       1798732800,
		UsageUpdatedAt: "2026-11-17T07:30:00+08:00",
	}

	view := SubscriptionViewFrom(sub)

	if view.UploadBytes != sub.UploadBytes || view.DownloadBytes != sub.DownloadBytes {
		t.Fatalf("usage bytes not mapped: %+v", view)
	}
	if view.TotalBytes != sub.TotalBytes || view.ExpireAt != sub.ExpireAt {
		t.Fatalf("quota fields not mapped: %+v", view)
	}
	if view.UsageUpdatedAt != sub.UsageUpdatedAt {
		t.Fatalf("usage timestamp not mapped: %+v", view)
	}
}

func TestNodeViewFromIncludesTagAndManual(t *testing.T) {
	subNode := Node{ID: "n1", SubscriptionID: "sub-1", Name: "节点", Enabled: true, Favorite: true, Tag: "流媒体"}
	view := NodeViewFrom(subNode, "主订阅", "n1")
	if view.Tag != "流媒体" || !view.Favorite || view.Manual {
		t.Fatalf("node view fields not mapped: %+v", view)
	}

	manual := Node{ID: "n2", Name: "手动"}
	if got := NodeViewFrom(manual, "", ""); !got.Manual || got.Tag != "" {
		t.Fatalf("manual node not marked: %+v", got)
	}
}
