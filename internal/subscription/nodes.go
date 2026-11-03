package subscription

import (
	"fmt"

	"github.com/MardukPrime/Fast-V/internal/model"
)

// BuildNodes converts parsed links into storage-safe node records.
func BuildNodes(subscriptionID string, parsed []ParsedNode) []model.Node {
	nodes := make([]model.Node, 0, len(parsed))
	for _, item := range parsed {
		sourceKey := item.Raw
		if sourceKey == "" {
			sourceKey = fmt.Sprintf("%s|%s|%d|%s", item.Protocol, item.Address, item.Port, item.Name)
		}
		nodes = append(nodes, model.Node{
			ID:             model.NewID(),
			SubscriptionID: subscriptionID,
			Name:           item.Name,
			Protocol:       item.Protocol,
			Address:        item.Address,
			Port:           item.Port,
			Raw:            item.Raw,
			Enabled:        true,
			SourceKey:      model.SourceKey(sourceKey),
		})
	}
	return nodes
}
