package model

import "time"

// Session is one sign-in, as shown to the person it belongs to.
//
// There is no token here and never will be: the token is not stored, so
// there is nothing to leak through this shape even by accident.
type Session struct {
	ID string `json:"id"`
	// IP and UserAgent are recorded so somebody can recognize their own
	// sessions. Both are attacker-controlled strings that are only ever
	// displayed.
	IP        string `json:"ip"`
	UserAgent string `json:"userAgent"`
	// Current marks the session making the request, so the screen can say
	// which row is the reader rather than letting them end it by surprise.
	Current bool `json:"current"`

	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}
