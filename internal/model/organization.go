package model

import "time"

// Organization is a single flat grouping of users. The MVP has no
// hierarchy, no org-scoped permissions, and no owner — see §3.4.3 for the
// full list of what is deliberately excluded.
type Organization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Code      string    `json:"code"`
	Remark    string    `json:"remark"`
	Status    Status    `json:"status"`
	SortOrder int       `json:"sortOrder"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// UserCount is populated by list endpoints so the UI can show how many
	// accounts a disable would affect.
	UserCount int64 `json:"userCount"`
}
