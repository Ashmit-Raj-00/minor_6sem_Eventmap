package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"eventmap/internal/geo"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrForbidden     = errors.New("forbidden")
)

type MemoryConfig struct {
	PasswordIterations int
}

type Memory struct {
	mu sync.RWMutex

	passwordIterations int

	usersByID    map[string]User
	usersByEmail map[string]string

	eventsByID   map[string]Event
	sessions     map[string][]Session
	participants map[string]map[string]Participant // eventID -> userID -> participant

	userPoints  map[string]int                  // userID -> points
	eventPoints map[string]map[string]int       // eventID -> userID -> points earned for the event
	checkins    map[string]map[string]time.Time // eventID -> userID -> checked in at
}

func NewMemory(cfg MemoryConfig) *Memory {
	if cfg.PasswordIterations <= 0 {
		cfg.PasswordIterations = 120_000
	}
	return &Memory{
		passwordIterations: cfg.PasswordIterations,
		usersByID:          map[string]User{},
		usersByEmail:       map[string]string{},
		eventsByID:         map[string]Event{},
		sessions:           map[string][]Session{},
		participants:       map[string]map[string]Participant{},
		userPoints:         map[string]int{},
		eventPoints:        map[string]map[string]int{},
		checkins:           map[string]map[string]time.Time{},
	}
}

func (m *Memory) CreateUser(email, password string, role Role) (User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || password == "" {
		return User{}, errors.New("email and password required")
	}
	if role != RoleAdmin && role != RoleOrganizer && role != RoleAttendee {
		return User{}, errors.New("invalid role")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.usersByEmail[email]; ok {
		return User{}, ErrAlreadyExists
	}

	salt := randomID(16)
	u := User{
		ID:           randomID(18),
		Email:        email,
		Role:         role,
		Salt:         salt,
		PasswordHash: hashPassword(password, salt, m.passwordIterations),
		CreatedAt:    time.Now(),
	}
	m.usersByID[u.ID] = u
	m.usersByEmail[email] = u.ID
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
	return u, nil
}

func (m *Memory) GetUserByID(id string) (User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.usersByID[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

func (m *Memory) VerifyPassword(email, password string) (User, error) {
	u, err := m.GetUserByEmail(email)
	if err != nil {
		return User{}, err
	}
	expected := hashPassword(password, u.Salt, m.passwordIterations)
	if !equalBytes(expected, u.PasswordHash) {
		return User{}, ErrForbidden
	}
	return u, nil
}

func (m *Memory) CreateEvent(e Event) (Event, error) {
	if strings.TrimSpace(e.Title) == "" {
		return Event{}, errors.New("title required")
	}
	if e.CreatedBy == "" {
		return Event{}, errors.New("created_by required")
	}
	e.Tags = normalizeTags(e.Tags)
	if e.CheckinRadiusKm <= 0 {
		e.CheckinRadiusKm = 0.2
	}
	e.ID = randomID(18)
	e.CreatedAt = time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventsByID[e.ID] = e
	m.addPointsLocked(e.CreatedBy, e.ID, 50)
	return e, nil
}

func (m *Memory) ListEvents() []Event {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Event, 0, len(m.eventsByID))
	for _, e := range m.eventsByID {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartsAt.Before(out[j].StartsAt) })
	return out
}

func (m *Memory) NearbyEvents(lat, lng, radiusKm float64) []Event {
	all := m.ListEvents()
	if radiusKm <= 0 {
		return all
	}
	out := make([]Event, 0, len(all))
	for _, e := range all {
		if geo.DistanceKm(lat, lng, e.Lat, e.Lng) <= radiusKm {
			out = append(out, e)
		}
	}
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

func (m *Memory) CreateSession(s Session) (Session, error) {
	if s.EventID == "" {
		return Session{}, errors.New("event_id required")
	}
	if strings.TrimSpace(s.Title) == "" {
		return Session{}, errors.New("title required")
	}
	if _, err := m.GetEvent(s.EventID); err != nil {
		return Session{}, err
	}

	s.ID = randomID(18)
	s.CreatedAt = time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.EventID] = append(m.sessions[s.EventID], s)
	sort.Slice(m.sessions[s.EventID], func(i, j int) bool {
		return m.sessions[s.EventID][i].StartsAt.Before(m.sessions[s.EventID][j].StartsAt)
	})
	return s, nil
}

func (m *Memory) ListSessions(eventID string) ([]Session, error) {
	if _, err := m.GetEvent(eventID); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := m.sessions[eventID]
	out := make([]Session, len(s))
	copy(out, s)
	return out, nil
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
	m.addPointsLocked(userID, eventID, 10)
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

func (m *Memory) TagEventNear(eventID, userID, tag string, lat, lng float64) (Event, error) {
	if eventID == "" || userID == "" {
		return Event{}, errors.New("event_id and user_id required")
	}
	if _, err := m.GetUserByID(userID); err != nil {
		return Event{}, err
	}
	tag = normalizeTag(tag)
	if tag == "" {
		return Event{}, errors.New("tag required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.eventsByID[eventID]
	if !ok {
		return Event{}, ErrNotFound
	}
	if geo.DistanceKm(lat, lng, e.Lat, e.Lng) > 0.75 {
		return Event{}, ErrForbidden
	}

	for _, t := range e.Tags {
		if t == tag {
			return e, nil
		}
	}
	e.Tags = append(e.Tags, tag)
	sort.Strings(e.Tags)
	m.eventsByID[eventID] = e
	m.addPointsLocked(userID, eventID, 5)
	return e, nil
}

func (m *Memory) CheckIn(eventID, userID string, lat, lng float64) (time.Time, error) {
	if eventID == "" || userID == "" {
		return time.Time{}, errors.New("event_id and user_id required")
	}
	if _, err := m.GetUserByID(userID); err != nil {
		return time.Time{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.eventsByID[eventID]
	if !ok {
		return time.Time{}, ErrNotFound
	}
	r := e.CheckinRadiusKm
	if r <= 0 {
		r = 0.2
	}
	if geo.DistanceKm(lat, lng, e.Lat, e.Lng) > r {
		return time.Time{}, ErrForbidden
	}
	if m.checkins[eventID] == nil {
		m.checkins[eventID] = map[string]time.Time{}
	}
	if _, ok := m.checkins[eventID][userID]; ok {
		return time.Time{}, ErrAlreadyExists
	}
	now := time.Now()
	m.checkins[eventID][userID] = now
	m.addPointsLocked(userID, eventID, 30)
	return now, nil
}

func (m *Memory) UserScore(userID string) (points int, level int, nextLevelAt int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	points = m.userPoints[userID]
	level = scoreLevel(points)
	nextLevelAt = level * 100
	return points, level, nextLevelAt
}

func (m *Memory) Leaderboard(limit int) []Score {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Score, 0, len(m.userPoints))
	for userID, pts := range m.userPoints {
		out = append(out, Score{UserID: userID, Points: pts})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Points == out[j].Points {
			return out[i].UserID < out[j].UserID
		}
		return out[i].Points > out[j].Points
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
	mm := m.eventPoints[eventID]
	out := make([]Score, 0, len(mm))
	for userID, pts := range mm {
		out = append(out, Score{UserID: userID, Points: pts})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Points == out[j].Points {
			return out[i].UserID < out[j].UserID
		}
		return out[i].Points > out[j].Points
	})
	if limit <= 0 || limit > len(out) {
		return out, nil
	}
	return out[:limit], nil
}

func (m *Memory) addPointsLocked(userID, eventID string, points int) {
	if userID == "" || points == 0 {
		return
	}
	m.userPoints[userID] += points
	if eventID != "" {
		if m.eventPoints[eventID] == nil {
			m.eventPoints[eventID] = map[string]int{}
		}
		m.eventPoints[eventID][userID] += points
	}
}

func scoreLevel(points int) int {
	if points < 0 {
		return 1
	}
	return (points / 100) + 1
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

func hashPassword(password, salt string, iterations int) []byte {
	if iterations <= 1 {
		iterations = 1
	}
	sum := sha256.Sum256([]byte(salt + ":" + password))
	out := sum[:]
	for i := 0; i < iterations; i++ {
		n := sha256.Sum256(out)
		out = n[:]
	}
	final := make([]byte, len(out))
	copy(final, out)
	return final
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
