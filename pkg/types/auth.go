package types

import (
	"time"
)

// AuthStatus represents the authentication status for a service
type AuthStatus struct {
	Service        string     `json:"service"`
	TokenSource    string     `json:"token_source,omitempty"`
	Authenticated  bool       `json:"authenticated"`
	User           *UserInfo  `json:"user,omitempty"`
	RateLimit      *RateLimit `json:"rate_limit,omitempty"`
	Error          string     `json:"error,omitempty"`
	HasPermissions bool       `json:"has_permissions"`
}

// UserInfo represents authenticated user information
type UserInfo struct {
	Username  string     `json:"username"`
	Name      string     `json:"name,omitempty"`
	Email     string     `json:"email,omitempty"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	Company   string     `json:"company,omitempty"`
}

// RateLimit represents API rate limit information
type RateLimit struct {
	Remaining int        `json:"remaining"`
	Total     int        `json:"total"`
	ResetTime *time.Time `json:"reset_time"`
}
