// Package azureid parses Azure resource IDs without the Azure SDK.
package azureid

import (
	"fmt"
	"strings"
)

// Ref is a parsed Azure resource ID.
type Ref struct {
	Subscription string
	Group        string
	Provider     string
	Type         string
	Name         string
	SubType      string
	SubName      string
}

// ResourceType returns Provider/Type, for example Microsoft.Web/sites.
func (r Ref) ResourceType() string {
	if r.Provider == "" || r.Type == "" {
		return ""
	}
	return r.Provider + "/" + r.Type
}

// NameOrID returns the last name segment, or the original id when parsing fails.
func NameOrID(id string) string {
	ref, err := Parse(id)
	if err != nil || ref.Name == "" {
		return id
	}
	return ref.Name
}

// Parse splits an Azure resource ID. Comparison of the static keys is
// case-insensitive, matching Azure Resource Manager.
func Parse(id string) (Ref, error) {
	if id == "" {
		return Ref{}, fmt.Errorf("azure id: empty")
	}
	parts := strings.Split(strings.Trim(id, "/"), "/")
	if len(parts) < 8 {
		return Ref{}, fmt.Errorf("azure id: %q is too short", id)
	}
	var ref Ref
	for i := 0; i+1 < len(parts); i += 2 {
		key := strings.ToLower(parts[i])
		val := parts[i+1]
		switch key {
		case "subscriptions":
			ref.Subscription = val
		case "resourcegroups":
			ref.Group = val
		case "providers":
			ref.Provider = val
			if i+3 < len(parts) {
				ref.Type = parts[i+2]
				ref.Name = parts[i+3]
				i += 2
			}
		default:
			if ref.SubType == "" {
				ref.SubType = parts[i]
				ref.SubName = val
			}
		}
	}
	if ref.Subscription == "" || ref.Group == "" || ref.Name == "" {
		return Ref{}, fmt.Errorf("azure id: %q is missing subscription, group, or name", id)
	}
	return ref, nil
}
