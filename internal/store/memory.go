package store

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrForbidden     = errors.New("forbidden")
)

type Memory struct {
	mu sync.RWMutex

	usersByID    map[string]User
	usersByEmail map[string]string

	eventsByID   map[string]Event
	participants map[string]map[string]Participant // eventID -> userID -> participant

	tasksByID    map[string]Task
	tasksByEvent map[string][]string // eventID -> task IDs

	submissionsByID   map[string]Submission
	submissionsByTask map[string][]string // taskID -> submission IDs (oldest..newest)

	userXP   map[string]int            // userID -> xp
	eventXP  map[string]map[string]int // eventID -> userID -> xp earned for the event
	xpLogs   []XPLog
	chatByEv map[string][]ChatMessage

	queriesByEv map[string][]Query

	notificationsByUser map[string][]Notification // userID -> newest-first
}

func NewMemory() *Memory {
	return &Memory{
		usersByID:            map[string]User{},
		usersByEmail:         map[string]string{},
		eventsByID:           map[string]Event{},
		participants:         map[string]map[string]Participant{},
		tasksByID:            map[string]Task{},
		tasksByEvent:         map[string][]string{},
		submissionsByID:      map[string]Submission{},
		submissionsByTask:    map[string][]string{},
		userXP:               map[string]int{},
		eventXP:              map[string]map[string]int{},
		chatByEv:             map[string][]ChatMessage{},
		queriesByEv:          map[string][]Query{},
		notificationsByUser:  map[string][]Notification{},
	}
}

func (m *Memory) UpsertUserFromOAuth(name, email, photoURL string) (User, bool, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	name = strings.TrimSpace(name)
	photoURL = strings.TrimSpace(photoURL)
	if email == "" {
		return User{}, false, errors.New("email required")
	}
	if name == "" {
		name = strings.Split(email, "@")[0]
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if id, ok := m.usersByEmail[email]; ok {
		u, ok := m.usersByID[id]
		if !ok {
			return User{}, false, ErrNotFound
		}
		changed := false
		if name != "" && u.Name != name {
			u.Name = name
			changed = true
		}
		if photoURL != "" && u.PhotoURL != photoURL {
			u.PhotoURL = photoURL
			changed = true
		}
		if changed {
			m.usersByID[u.ID] = u
		}
		u.XP = m.userXP[u.ID]
		return u, false, nil
	}

	u := User{
		ID:        randomID(18),
		Name:      name,
		Email:     email,
		PhotoURL:  photoURL,
		Role:      RoleOperator,
		CreatedAt: time.Now(),
	}
	m.usersByID[u.ID] = u
	m.usersByEmail[email] = u.ID
	return u, true, nil
}

func (m *Memory) UpdateUserRole(userID string, role Role) (User, error) {
	if role != RoleOperator && role != RoleCommander {
		return User{}, errors.New("invalid role")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.usersByID[userID]
	if !ok {
		return User{}, ErrNotFound
	}
	u.Role = role
	m.usersByID[userID] = u
	u.XP = m.userXP[u.ID]
	return u, nil
}

func (m *Memory) GetUserByID(id string) (User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.usersByID[id]
	if !ok {
		return User{}, ErrNotFound
	}
	u.XP = m.userXP[u.ID]
	return u, nil
}

func (m *Memory) GetUserByEmail(email string) (User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.usersByEmail[email]
	if !ok {
		return User{}, ErrNotFound
	}
	u, ok := m.usersByID[id]
	if !ok {
		return User{}, ErrNotFound
	}
	u.XP = m.userXP[u.ID]
	return u, nil
}

func (m *Memory) CreateEvent(e Event) (Event, error) {
	if strings.TrimSpace(e.Title) == "" {
		return Event{}, errors.New("title required")
	}
	if e.CreatedBy == "" {
		return Event{}, errors.New("created_by required")
	}
	if e.Visibility == "" {
		e.Visibility = EventPublic
	}
	if e.Visibility != EventPublic && e.Visibility != EventPrivate {
		return Event{}, errors.New("invalid visibility")
	}
	if e.Status == "" {
		e.Status = EventActive
	}
	if e.Status != EventDraft && e.Status != EventActive && e.Status != EventCompleted && e.Status != EventArchived {
		return Event{}, errors.New("invalid status")
	}
	e.Tags = normalizeTags(e.Tags)
	e.ID = randomID(18)
	e.CreatedAt = time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventsByID[e.ID] = e
	return e, nil
}

func (m *Memory) UpdateEvent(eventID, userID string, patch func(Event) (Event, error)) (Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.eventsByID[eventID]
	if !ok {
		return Event{}, ErrNotFound
	}
	if e.CreatedBy != userID {
		return Event{}, ErrForbidden
	}
	updated, err := patch(e)
	if err != nil {
		return Event{}, err
	}
	updated.Tags = normalizeTags(updated.Tags)
	m.eventsByID[eventID] = updated
	return updated, nil
}

func (m *Memory) ListEvents(viewerID string) []Event {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Event, 0, len(m.eventsByID))
	for _, e := range m.eventsByID {
		if e.Visibility == EventPrivate && viewerID != "" {
			if !m.isParticipantLocked(e.ID, viewerID) && e.CreatedBy != viewerID {
				continue
			}
		} else if e.Visibility == EventPrivate && viewerID == "" {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartsAt.Before(out[j].StartsAt) })
	return out
}

func (m *Memory) GetEvent(id string) (Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.eventsByID[id]
	if !ok {
		return Event{}, ErrNotFound
	}
	return e, nil
}

func (m *Memory) JoinEvent(eventID, userID string) (Participant, error) {
	if eventID == "" || userID == "" {
		return Participant{}, errors.New("event_id and user_id required")
	}
	if _, err := m.GetEvent(eventID); err != nil {
		return Participant{}, err
	}
	if _, err := m.GetUserByID(userID); err != nil {
		return Participant{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.participants[eventID] == nil {
		m.participants[eventID] = map[string]Participant{}
	}
	if _, ok := m.participants[eventID][userID]; ok {
		return Participant{}, ErrAlreadyExists
	}
	p := Participant{EventID: eventID, UserID: userID, JoinedAt: time.Now()}
	m.participants[eventID][userID] = p
	return p, nil
}

func (m *Memory) ListParticipants(eventID string) ([]Participant, error) {
	if _, err := m.GetEvent(eventID); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	mm := m.participants[eventID]
	out := make([]Participant, 0, len(mm))
	for _, p := range mm {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].JoinedAt.Before(out[j].JoinedAt) })
	return out, nil
}

func (m *Memory) IsParticipant(eventID, userID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isParticipantLocked(eventID, userID)
}

func (m *Memory) isParticipantLocked(eventID, userID string) bool {
	if userID == "" {
		return false
	}
	if mm := m.participants[eventID]; mm != nil {
		_, ok := mm[userID]
		return ok
	}
	return false
}

func (m *Memory) CreateTask(t Task) (Task, error) {
	if t.EventID == "" {
		return Task{}, errors.New("event_id required")
	}
	if strings.TrimSpace(t.Title) == "" {
		return Task{}, errors.New("title required")
	}
	if t.CreatedBy == "" {
		return Task{}, errors.New("created_by required")
	}
	if _, err := m.GetEvent(t.EventID); err != nil {
		return Task{}, err
	}
	if t.Type == "" {
		t.Type = TaskOpen
	}
	if t.Type != TaskOpen && t.Type != TaskAssigned {
		return Task{}, errors.New("invalid task type")
	}
	if t.Priority == "" {
		t.Priority = PriorityMedium
	}
	if t.Priority != PriorityLow && t.Priority != PriorityMedium && t.Priority != PriorityHigh {
		return Task{}, errors.New("invalid priority")
	}
	if t.Difficulty <= 0 {
		t.Difficulty = 2
	}
	if t.Difficulty < 1 || t.Difficulty > 5 {
		return Task{}, errors.New("difficulty must be 1-5")
	}
	if t.Type == TaskAssigned && strings.TrimSpace(t.AssignedTo) == "" {
		return Task{}, errors.New("assigned_to required for assigned tasks")
	}

	now := time.Now()
	t.ID = randomID(18)
	t.Status = TaskPending
	t.CreatedAt = now
	t.UpdatedAt = now

	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasksByID[t.ID] = t
	m.tasksByEvent[t.EventID] = append(m.tasksByEvent[t.EventID], t.ID)
	return t, nil
}

func (m *Memory) ListTasks(eventID, viewerID string) ([]Task, error) {
	e, err := m.GetEvent(eventID)
	if err != nil {
		return nil, err
	}
	if viewerID == "" {
		return nil, ErrForbidden
	}
	if e.CreatedBy != viewerID && !m.IsParticipant(eventID, viewerID) {
		return nil, ErrForbidden
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := m.tasksByEvent[eventID]
	out := make([]Task, 0, len(ids))
	for _, id := range ids {
		t, ok := m.tasksByID[id]
		if !ok {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

func (m *Memory) GetTask(taskID string) (Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tasksByID[taskID]
	if !ok {
		return Task{}, ErrNotFound
	}
	return t, nil
}

func (m *Memory) StartTask(taskID, userID string) (Task, error) {
	if userID == "" {
		return Task{}, ErrForbidden
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasksByID[taskID]
	if !ok {
		return Task{}, ErrNotFound
	}
	e, ok := m.eventsByID[t.EventID]
	if !ok {
		return Task{}, ErrNotFound
	}
	if e.CreatedBy != userID && !m.isParticipantLocked(t.EventID, userID) {
		return Task{}, ErrForbidden
	}
	if t.Type == TaskAssigned && t.AssignedTo != userID {
		return Task{}, ErrForbidden
	}
	if t.Status != TaskPending && t.Status != TaskRejected {
		return Task{}, errors.New("task not startable")
	}
	now := time.Now()
	t.Status = TaskInProgress
	t.StartedBy = userID
	t.StartedAt = now
	t.UpdatedAt = now
	m.tasksByID[taskID] = t
	return t, nil
}

func (m *Memory) SubmitTask(taskID, userID, imageURL, comment string, lat, lng float64, hasGeo bool) (Task, Submission, error) {
	imageURL = strings.TrimSpace(imageURL)
	if imageURL == "" {
		return Task{}, Submission{}, errors.New("image_url required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasksByID[taskID]
	if !ok {
		return Task{}, Submission{}, ErrNotFound
	}
	if t.Status != TaskInProgress {
		return Task{}, Submission{}, errors.New("task not in progress")
	}
	if t.StartedBy != userID {
		return Task{}, Submission{}, ErrForbidden
	}

	now := time.Now()
	sub := Submission{
		ID:         randomID(18),
		TaskID:     taskID,
		EventID:    t.EventID,
		OperatorID: userID,
		ImageURL:   imageURL,
		Comment:    strings.TrimSpace(comment),
		Lat:        lat,
		Lng:        lng,
		HasGeo:     hasGeo,
		Status:     SubmissionSubmitted,
		CreatedAt:  now,
	}
	m.submissionsByID[sub.ID] = sub
	m.submissionsByTask[taskID] = append(m.submissionsByTask[taskID], sub.ID)

	t.Status = TaskSubmitted
	t.SubmittedAt = now
	t.UpdatedAt = now
	m.tasksByID[taskID] = t
	return t, sub, nil
}

func (m *Memory) LatestSubmission(taskID string) (Submission, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := m.submissionsByTask[taskID]
	if len(ids) == 0 {
		return Submission{}, ErrNotFound
	}
	sub, ok := m.submissionsByID[ids[len(ids)-1]]
	if !ok {
		return Submission{}, ErrNotFound
	}
	return sub, nil
}

func (m *Memory) ReviewLatestSubmission(taskID, commanderID string, approve bool, feedback string, quality int) (Task, Submission, int, error) {
	feedback = strings.TrimSpace(feedback)
	if quality < 0 || quality > 5 {
		return Task{}, Submission{}, 0, errors.New("quality must be 0-5")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasksByID[taskID]
	if !ok {
		return Task{}, Submission{}, 0, ErrNotFound
	}
	e, ok := m.eventsByID[t.EventID]
	if !ok {
		return Task{}, Submission{}, 0, ErrNotFound
	}
	if e.CreatedBy != commanderID {
		return Task{}, Submission{}, 0, ErrForbidden
	}
	if t.Status != TaskSubmitted {
		return Task{}, Submission{}, 0, errors.New("task not submitted")
	}
	ids := m.submissionsByTask[taskID]
	if len(ids) == 0 {
		return Task{}, Submission{}, 0, ErrNotFound
	}
	sub, ok := m.submissionsByID[ids[len(ids)-1]]
	if !ok {
		return Task{}, Submission{}, 0, ErrNotFound
	}
	if sub.Status != SubmissionSubmitted {
		return Task{}, Submission{}, 0, errors.New("submission already reviewed")
	}

	now := time.Now()
	sub.Feedback = feedback
	sub.ReviewedBy = commanderID
	sub.ReviewedAt = now
	if quality > 0 {
		sub.Quality = quality
	}

	var awarded int
	if approve {
		sub.Status = SubmissionApproved
		t.Status = TaskCompleted
		t.CompletedAt = now
		awarded = calculateXP(t, sub)
		m.addXPLocked(sub.OperatorID, t.EventID, t.ID, awarded, "task_approved")
	} else {
		sub.Status = SubmissionRejected
		t.Status = TaskRejected
		t.LastFeedback = feedback
	}

	t.UpdatedAt = now
	m.submissionsByID[sub.ID] = sub
	m.tasksByID[t.ID] = t
	return t, sub, awarded, nil
}

func calculateXP(t Task, s Submission) int {
	// Deterministic, explainable, and easy to tune.
	baseByPriority := map[TaskPriority]int{
		PriorityLow:    20,
		PriorityMedium: 40,
		PriorityHigh:   70,
	}
	base := baseByPriority[t.Priority]
	if base == 0 {
		base = 40
	}
	base += (t.Difficulty - 1) * 15 // 1..5 => +0..+60

	qualityBonus := 0
	if s.Quality > 0 {
		qualityBonus = (s.Quality - 1) * 5 // 1..5 => +0..+20
	}

	speedBonus := 0
	if !t.StartedAt.IsZero() && !t.SubmittedAt.IsZero() {
		d := t.SubmittedAt.Sub(t.StartedAt)
		switch {
		case d <= 1*time.Hour:
			speedBonus = 15
		case d <= 6*time.Hour:
			speedBonus = 8
		}
	}

	xp := base + qualityBonus + speedBonus
	if xp < 5 {
		xp = 5
	}
	return xp
}

func (m *Memory) addXPLocked(userID, eventID, taskID string, amount int, reason string) {
	if userID == "" || amount == 0 {
		return
	}
	m.userXP[userID] += amount
	if eventID != "" {
		if m.eventXP[eventID] == nil {
			m.eventXP[eventID] = map[string]int{}
		}
		m.eventXP[eventID][userID] += amount
	}
	m.xpLogs = append(m.xpLogs, XPLog{
		ID:        randomID(18),
		UserID:    userID,
		EventID:   eventID,
		TaskID:    taskID,
		Amount:    amount,
		Reason:    reason,
		CreatedAt: time.Now(),
	})
}

func (m *Memory) UserXP(userID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.userXP[userID]
}

func (m *Memory) Leaderboard(limit int) []Score {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Score, 0, len(m.userXP))
	for userID, xp := range m.userXP {
		out = append(out, Score{UserID: userID, XP: xp})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].XP == out[j].XP {
			return out[i].UserID < out[j].UserID
		}
		return out[i].XP > out[j].XP
	})
	if limit <= 0 || limit > len(out) {
		return out
	}
	return out[:limit]
}

func (m *Memory) EventLeaderboard(eventID string, limit int) ([]Score, error) {
	if _, err := m.GetEvent(eventID); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	mm := m.eventXP[eventID]
	out := make([]Score, 0, len(mm))
	for userID, xp := range mm {
		out = append(out, Score{UserID: userID, XP: xp})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].XP == out[j].XP {
			return out[i].UserID < out[j].UserID
		}
		return out[i].XP > out[j].XP
	})
	if limit <= 0 || limit > len(out) {
		return out, nil
	}
	return out[:limit], nil
}

func (m *Memory) EventDashboard(eventID, commanderID string) (map[string]any, error) {
	e, err := m.GetEvent(eventID)
	if err != nil {
		return nil, err
	}
	if e.CreatedBy != commanderID {
		return nil, ErrForbidden
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	taskIDs := m.tasksByEvent[eventID]
	total := len(taskIDs)
	completed := 0
	submitted := 0
	inProgress := 0
	for _, id := range taskIDs {
		t, ok := m.tasksByID[id]
		if !ok {
			continue
		}
		switch t.Status {
		case TaskCompleted:
			completed++
		case TaskSubmitted:
			submitted++
		case TaskInProgress:
			inProgress++
		}
	}

	progressPct := 0
	if e.GoalTarget > 0 {
		if completed >= e.GoalTarget {
			progressPct = 100
		} else {
			progressPct = int(float64(completed) / float64(e.GoalTarget) * 100.0)
		}
	} else if total > 0 {
		progressPct = int(float64(completed) / float64(total) * 100.0)
	}

	activeParticipants := 0
	for _, p := range m.participants[eventID] {
		_ = p
		activeParticipants++
	}

	top := make([]Score, 0, len(m.eventXP[eventID]))
	for userID, xp := range m.eventXP[eventID] {
		top = append(top, Score{UserID: userID, XP: xp})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].XP > top[j].XP })
	if len(top) > 10 {
		top = top[:10]
	}

	return map[string]any{
		"eventId":           eventID,
		"totalTasks":        total,
		"completedTasks":    completed,
		"inProgressTasks":   inProgress,
		"pendingApprovals":  submitted,
		"activeParticipants": activeParticipants,
		"progressPct":       progressPct,
		"topContributors":   top,
	}, nil
}

func (m *Memory) AddChatMessage(eventID, userID, body string) (ChatMessage, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return ChatMessage{}, errors.New("body required")
	}
	e, err := m.GetEvent(eventID)
	if err != nil {
		return ChatMessage{}, err
	}
	if e.CreatedBy != userID && !m.IsParticipant(eventID, userID) {
		return ChatMessage{}, ErrForbidden
	}

	msg := ChatMessage{
		ID:        randomID(18),
		EventID:   eventID,
		UserID:    userID,
		Body:      body,
		CreatedAt: time.Now(),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.chatByEv[eventID] = append(m.chatByEv[eventID], msg)
	return msg, nil
}

func (m *Memory) ListChatMessages(eventID, userID string, limit int) ([]ChatMessage, error) {
	e, err := m.GetEvent(eventID)
	if err != nil {
		return nil, err
	}
	if e.CreatedBy != userID && !m.IsParticipant(eventID, userID) {
		return nil, ErrForbidden
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	all := m.chatByEv[eventID]
	if len(all) <= limit {
		out := make([]ChatMessage, len(all))
		copy(out, all)
		return out, nil
	}
	out := make([]ChatMessage, limit)
	copy(out, all[len(all)-limit:])
	return out, nil
}

func (m *Memory) CreateQuery(eventID, fromUserID, body string) (Query, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Query{}, errors.New("body required")
	}
	e, err := m.GetEvent(eventID)
	if err != nil {
		return Query{}, err
	}
	if e.CreatedBy != fromUserID && !m.IsParticipant(eventID, fromUserID) {
		return Query{}, ErrForbidden
	}
	q := Query{
		ID:         randomID(18),
		EventID:    eventID,
		FromUserID: fromUserID,
		Body:       body,
		Status:     QueryOpen,
		CreatedAt:  time.Now(),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queriesByEv[eventID] = append(m.queriesByEv[eventID], q)
	return q, nil
}

func (m *Memory) AnswerQuery(eventID, queryID, commanderID, answer string) (Query, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return Query{}, errors.New("answer required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.eventsByID[eventID]
	if !ok {
		return Query{}, ErrNotFound
	}
	if e.CreatedBy != commanderID {
		return Query{}, ErrForbidden
	}
	qs := m.queriesByEv[eventID]
	for i := range qs {
		if qs[i].ID != queryID {
			continue
		}
		if qs[i].Status != QueryOpen {
			return Query{}, errors.New("query already answered")
		}
		qs[i].Status = QueryAnswered
		qs[i].Answer = answer
		qs[i].AnsweredBy = commanderID
		qs[i].AnsweredAt = time.Now()
		m.queriesByEv[eventID] = qs
		return qs[i], nil
	}
	return Query{}, ErrNotFound
}

func (m *Memory) ListQueries(eventID, userID string) ([]Query, error) {
	e, err := m.GetEvent(eventID)
	if err != nil {
		return nil, err
	}
	if e.CreatedBy != userID && !m.IsParticipant(eventID, userID) {
		return nil, ErrForbidden
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := m.queriesByEv[eventID]
	out := make([]Query, len(all))
	copy(out, all)
	return out, nil
}

func (m *Memory) AddNotification(userID, kind string, payload map[string]any) Notification {
	n := Notification{
		ID:        randomID(18),
		UserID:    userID,
		Kind:      strings.TrimSpace(kind),
		Payload:   payload,
		CreatedAt: time.Now(),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notificationsByUser[userID] = append([]Notification{n}, m.notificationsByUser[userID]...)
	return n
}

func (m *Memory) ListNotifications(userID string, limit int) []Notification {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := m.notificationsByUser[userID]
	if len(all) <= limit {
		out := make([]Notification, len(all))
		copy(out, all)
		return out
	}
	out := make([]Notification, limit)
	copy(out, all[:limit])
	return out
}

func (m *Memory) MarkNotificationRead(userID, notificationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	all := m.notificationsByUser[userID]
	for i := range all {
		if all[i].ID == notificationID {
			if all[i].ReadAt.IsZero() {
				all[i].ReadAt = time.Now()
				m.notificationsByUser[userID] = all
			}
			return nil
		}
	}
	return ErrNotFound
}

func normalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		n := normalizeTag(t)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeTag(tag string) string {
	tag = strings.TrimSpace(strings.ToLower(tag))
	if tag == "" {
		return ""
	}
	tag = strings.ReplaceAll(tag, " ", "-")
	if len(tag) > 24 {
		tag = tag[:24]
	}
	var b strings.Builder
	b.Grow(len(tag))
	for _, r := range tag {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "-_.")
	if out == "" {
		return ""
	}
	return out
}

func randomID(nbytes int) string {
	b := make([]byte, nbytes)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

