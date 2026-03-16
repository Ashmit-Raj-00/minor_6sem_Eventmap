package store

import "time"

type Role string

const (
	RoleAdmin     Role = "admin"
	RoleOrganizer Role = "organizer"
	RoleAttendee  Role = "attendee"
)

type User struct {
	ID           string
	Email        string
	Role         Role
	Salt         string
	PasswordHash []byte
	CreatedAt    time.Time
}

type Event struct {
	ID          string
	Title       string
	Description string
	StartsAt    time.Time
	EndsAt      time.Time
	Lat         float64
	Lng         float64
	Address     string
	CreatedBy   string
	CreatedAt   time.Time
}

type Session struct {
	ID        string
	EventID   string
	Title     string
	StartsAt  time.Time
	EndsAt    time.Time
	CreatedAt time.Time
}

type Participant struct {
	UserID   string
	EventID  string
	JoinedAt time.Time
}

