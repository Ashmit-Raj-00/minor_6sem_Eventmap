package store

import "time"

type Role string

const (
	RoleCommander Role = "commander"
	RoleOperator  Role = "operator"
)

type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	PhotoURL  string    `json:"photoUrl,omitempty"`
	Role      Role      `json:"role"`
	XP        int       `json:"xp"`
	CreatedAt time.Time `json:"createdAt"`
}

type EventVisibility string

const (
	EventPublic  EventVisibility = "public"
	EventPrivate EventVisibility = "private"
)

type EventStatus string

const (
	EventDraft     EventStatus = "draft"
	EventActive    EventStatus = "active"
	EventCompleted EventStatus = "completed"
	EventArchived  EventStatus = "archived"
)

type Event struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	Description  string          `json:"description"`
	Goal         string          `json:"goal,omitempty"`
	GoalTarget   int             `json:"goalTarget,omitempty"`
	GoalUnit     string          `json:"goalUnit,omitempty"`
	Instructions string          `json:"instructions,omitempty"`
	Visibility   EventVisibility `json:"visibility"`
	Status       EventStatus     `json:"status"`
	StartsAt     time.Time       `json:"startsAt"`
	EndsAt       time.Time       `json:"endsAt"`
	Lat          float64         `json:"lat"`
	Lng          float64         `json:"lng"`
	Address      string          `json:"address"`
	Tags         []string        `json:"tags,omitempty"`
	CreatedBy    string          `json:"createdBy"`
	CreatedAt    time.Time       `json:"createdAt"`
}

type Participant struct {
	UserID   string    `json:"userId"`
	EventID  string    `json:"eventId"`
	JoinedAt time.Time `json:"joinedAt"`
}

type TaskType string

const (
	TaskOpen     TaskType = "open"
	TaskAssigned TaskType = "assigned"
)

type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskInProgress TaskStatus = "in_progress"
	TaskSubmitted  TaskStatus = "submitted"
	TaskCompleted  TaskStatus = "completed"
	TaskRejected   TaskStatus = "rejected"
)

type TaskPriority string

const (
	PriorityLow    TaskPriority = "low"
	PriorityMedium TaskPriority = "medium"
	PriorityHigh   TaskPriority = "high"
)

type Task struct {
	ID           string       `json:"id"`
	EventID      string       `json:"eventId"`
	Title        string       `json:"title"`
	Description  string       `json:"description"`
	Type         TaskType     `json:"type"`
	Priority     TaskPriority `json:"priority"`
	Difficulty   int          `json:"difficulty"` // 1-5
	Deadline     time.Time    `json:"deadline,omitempty"`
	Lat          float64      `json:"lat,omitempty"`
	Lng          float64      `json:"lng,omitempty"`
	HasLocation  bool         `json:"hasLocation,omitempty"`
	AssignedTo   string       `json:"assignedTo,omitempty"`
	Status       TaskStatus   `json:"status"`
	StartedBy    string       `json:"startedBy,omitempty"`
	StartedAt    time.Time    `json:"startedAt,omitempty"`
	SubmittedAt  time.Time    `json:"submittedAt,omitempty"`
	CompletedAt  time.Time    `json:"completedAt,omitempty"`
	LastFeedback string       `json:"lastFeedback,omitempty"`
	CreatedBy    string       `json:"createdBy"`
	CreatedAt    time.Time    `json:"createdAt"`
	UpdatedAt    time.Time    `json:"updatedAt"`
}

type SubmissionStatus string

const (
	SubmissionSubmitted SubmissionStatus = "submitted"
	SubmissionApproved  SubmissionStatus = "approved"
	SubmissionRejected  SubmissionStatus = "rejected"
)

type Submission struct {
	ID         string           `json:"id"`
	TaskID     string           `json:"taskId"`
	EventID    string           `json:"eventId"`
	OperatorID string           `json:"operatorId"`
	ImageURL   string           `json:"imageUrl"`
	Comment    string           `json:"comment,omitempty"`
	Lat        float64          `json:"lat,omitempty"`
	Lng        float64          `json:"lng,omitempty"`
	HasGeo     bool             `json:"hasGeo,omitempty"`
	Status     SubmissionStatus `json:"status"`
	Quality    int              `json:"quality,omitempty"` // 1-5, optional
	Feedback   string           `json:"feedback,omitempty"`
	ReviewedBy string           `json:"reviewedBy,omitempty"`
	ReviewedAt time.Time        `json:"reviewedAt,omitempty"`
	CreatedAt  time.Time        `json:"createdAt"`
}

type XPLog struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	EventID   string    `json:"eventId,omitempty"`
	TaskID    string    `json:"taskId,omitempty"`
	Amount    int       `json:"amount"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"createdAt"`
}

type ChatMessage struct {
	ID        string    `json:"id"`
	EventID   string    `json:"eventId"`
	UserID    string    `json:"userId"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

type QueryStatus string

const (
	QueryOpen     QueryStatus = "open"
	QueryAnswered QueryStatus = "answered"
)

type Query struct {
	ID         string      `json:"id"`
	EventID    string      `json:"eventId"`
	FromUserID string      `json:"fromUserId"`
	Body       string      `json:"body"`
	Status     QueryStatus `json:"status"`
	Answer     string      `json:"answer,omitempty"`
	AnsweredBy string      `json:"answeredBy,omitempty"`
	AnsweredAt time.Time   `json:"answeredAt,omitempty"`
	CreatedAt  time.Time   `json:"createdAt"`
}

type Notification struct {
	ID        string         `json:"id"`
	UserID    string         `json:"userId"`
	Kind      string         `json:"kind"`
	Payload   map[string]any `json:"payload,omitempty"`
	ReadAt    time.Time      `json:"readAt,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

type Score struct {
	UserID string `json:"userId"`
	XP     int    `json:"xp"`
}
