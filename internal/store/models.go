package store

import "time"

type Role string

const (
	RoleAdmin     Role = "admin"
	RoleOrganizer Role = "organizer"
	RoleAttendee  Role = "attendee"
)

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Role         Role      `json:"role"`
	Salt         string    `json:"-"`
	PasswordHash []byte    `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Event struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	StartsAt        time.Time `json:"startsAt"`
	EndsAt          time.Time `json:"endsAt"`
	Lat             float64   `json:"lat"`
	Lng             float64   `json:"lng"`
	Address         string    `json:"address"`
	Tags            []string  `json:"tags,omitempty"`
	CheckinRadiusKm float64   `json:"checkinRadiusKm,omitempty"`
	CreatedBy       string    `json:"createdBy"`
	CreatedAt       time.Time `json:"createdAt"`
}

type Session struct {
	ID        string    `json:"id"`
	EventID   string    `json:"eventId"`
	Title     string    `json:"title"`
	StartsAt  time.Time `json:"startsAt"`
	EndsAt    time.Time `json:"endsAt"`
	CreatedAt time.Time `json:"createdAt"`
}

type Participant struct {
	UserID   string    `json:"userId"`
	EventID  string    `json:"eventId"`
	JoinedAt time.Time `json:"joinedAt"`
}

type Score struct {
	UserID string `json:"userId"`
	Points int    `json:"points"`
}
