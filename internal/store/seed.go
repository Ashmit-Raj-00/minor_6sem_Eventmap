package store

import "strings"

func SeedDefaultAdmin(m *Memory, username, password string) {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" || password == "" {
		return
	}
	_, _ = m.CreateUser(username, password, RoleAdmin)
}
