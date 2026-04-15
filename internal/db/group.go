package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const MaxGroups = 10

// ErrMaxGroups is returned when the group limit is reached.
var ErrMaxGroups = fmt.Errorf("maximum %d groups reached", MaxGroups)

// ErrGroupNotFound is returned when a group does not exist.
var ErrGroupNotFound = errors.New("group not found")

// Group represents a user-created feed group.
type Group struct {
	ID        int64
	Name      string
	Position  int
	CreatedAt time.Time
}

// ListGroups returns all groups ordered by position.
func (d *DB) ListGroups() ([]Group, error) {
	rows, err := d.Query(
		`SELECT id, name, position, created_at FROM groups ORDER BY position ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Position, &g.CreatedAt); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// CreateGroup creates a new group. Returns ErrMaxGroups if the limit is reached.
func (d *DB) CreateGroup(name string) (*Group, error) {
	n, err := d.countGroups()
	if err != nil {
		return nil, err
	}
	if n >= MaxGroups {
		return nil, ErrMaxGroups
	}
	res, err := d.Exec(
		`INSERT INTO groups (name, position) VALUES (?, ?)`, name, n,
	)
	if err != nil {
		return nil, fmt.Errorf("create group: %w", err)
	}
	id, _ := res.LastInsertId()
	return &Group{ID: id, Name: name, Position: n}, nil
}

// DeleteGroup deletes a group by ID.
func (d *DB) DeleteGroup(id int64) error {
	res, err := d.Exec(`DELETE FROM groups WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrGroupNotFound
	}
	return nil
}

// GetGroupByName looks up a group by name.
func (d *DB) GetGroupByName(name string) (*Group, error) {
	var g Group
	err := d.QueryRow(
		`SELECT id, name, position, created_at FROM groups WHERE name = ?`, name,
	).Scan(&g.ID, &g.Name, &g.Position, &g.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrGroupNotFound
	}
	return &g, err
}

func (d *DB) countGroups() (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM groups`).Scan(&n)
	return n, err
}
