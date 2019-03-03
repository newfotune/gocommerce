package models

import (
	"time"

	"gocommerce/assetstores"
)

// Download represents a purchased asset download.
type Download struct {
	ID int64 `json:"id"`

	OrderID    int64 `json:"order_id"`
	LineItemID int64 `json:"line_item_id"`

	Title  string `json:"title"`
	Sku    string `json:"sku"`
	Format string `json:"format"`
	URL    string `json:"url"`

	DownloadCount uint64 `json:"downloads"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"-" sql:"index"`
}

// TableName returns the database table name for the Download model.
func (Download) TableName() string {
	return tableName("downloads")
}

// SignURL signs a download URL using the provided asset store.
func (d *Download) SignURL(store assetstores.Store) error {
	signedURL, err := store.SignURL(d.URL)
	if err != nil {
		return err
	}
	d.URL = signedURL

	return nil
}
