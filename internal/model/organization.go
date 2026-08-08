package model

import "time"

// Organization is a single flat grouping of users. The MVP has no
// hierarchy, no org-scoped permissions, and no owner — see §3.4.3 for the
// full list of what is deliberately excluded.
type Organization struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Code   string `json:"code"`
	Remark string `json:"remark"`
	// ParentID is empty for a root. The list returns the tree flat, with
	// each row naming its parent, because a nested shape would have to be
	// unflattened by every consumer and cannot be sorted or filtered
	// without rebuilding it.
	ParentID  string    `json:"parentId"`
	Status    Status    `json:"status"`
	SortOrder int       `json:"sortOrder"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// UserCount is populated by list endpoints so the UI can show how many
	// accounts a disable would affect.
	UserCount int64 `json:"userCount"`
}
