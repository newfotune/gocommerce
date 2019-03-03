package models

import (
	"strings"
	"time"

	"github.com/jinzhu/gorm"
)

// Event represents a change to an order.
type Event struct {
	ID uint64 `json:"id"`

	IP string `json:"ip"`

	User   *User `json:"user,omitempty"`
	UserID int64 `json:"user_id,omitempty"`

	Order   *Order `json:"order,omitempty"`
	OrderID int64  `json:"order_id,omitempty"`

	Type    string `json:"type"`
	Changes string `json:"data"`

	CreatedAt time.Time `json:"created_at"`
}

// TableName returns the database table name for the Event model.
func (Event) TableName() string {
	return tableName("events")
}

// EventType is the type of change that occurred.
type EventType string

const (
	// EventCreated is the EventType when an order is created.
	EventCreated EventType = "created"
	// EventUpdated is the EventType when an order is updated.
	EventUpdated EventType = "updated"
	// EventDeleted is the EventType when an order is deleted.
	EventDeleted EventType = "deleted"
)

// LogEvent logs a new event
func LogEvent(db *gorm.DB, ip string, userID, orderID int, eventType EventType, changes []string) {
	event := &Event{
		IP:      ip,
		UserID:  userID,
		OrderID: orderID,
		Type:    string(eventType),
	}
	if changes != nil {
		event.Changes = strings.Join(changes, ",")
	}
	db.Create(event)
}
