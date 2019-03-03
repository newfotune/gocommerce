package api

import (
	"net/http"

	gcontext "gocommerce/context"
	"gocommerce/models"
)

type salesRow struct {
	Total    uint64 `json:"total"`
	SubTotal uint64 `json:"subtotal"`
	Taxes    uint64 `json:"taxes"`
	Currency string `json:"currency"`
	Orders   uint64 `json:"orders"`
}

type productsRow struct {
	Sku      string `json:"sku"`
	Path     string `json:"path"`
	Total    uint64 `json:"total"`
	Currency string `json:"currency"`
}

// SalesReport lists the sales numbers for a period
func (a *API) SalesReport(w http.ResponseWriter, r *http.Request) error {
	instanceID := gcontext.GetInstanceID(r.Context())

	query := a.db.
		Model(&models.Order{}).
		Select("sum(total) as total, sum(sub_total) as subtotal, sum(taxes) as taxes, currency, count(*) as orders").
		Where("payment_state = 'paid' AND instance_id = ?", instanceID).
		Group("currency")

	query, err := parseTimeQueryParams(query, query.NewScope(models.Order{}).QuotedTableName(), r.URL.Query())
	if err != nil {
		return badRequestError(err.Error())
	}

	rows, err := query.Rows()
	if err != nil {
		return internalServerError("Database error").WithInternalError(err)
	}
	defer rows.Close()
	result := []*salesRow{}
	for rows.Next() {
		row := &salesRow{}
		err = rows.Scan(&row.Total, &row.SubTotal, &row.Taxes, &row.Currency, &row.Orders)
		if err != nil {
			return internalServerError("Database error").WithInternalError(err)
		}
		result = append(result, row)
	}

	return sendJSON(w, http.StatusOK, result)
}

// ProductsReport list the products sold within a period
func (a *API) ProductsReport(w http.ResponseWriter, r *http.Request) error {
	instanceID := gcontext.GetInstanceID(r.Context())
	ordersTable := a.db.NewScope(models.Order{}).QuotedTableName()
	itemsTable := a.db.NewScope(models.LineItem{}).QuotedTableName()
	query := a.db.
		Model(&models.LineItem{}).
		Select("sku, path, sum(quantity * price) as total, currency").
		Joins("JOIN " + ordersTable + " ON " + ordersTable + ".id = " + itemsTable + ".order_id " + "AND " + ordersTable + ".payment_state = 'paid'").
		Group("sku, path, currency").
		Order("total desc")

	query = query.Where(ordersTable+".instance_id = ?", instanceID)
	from, to, err := getTimeQueryParams(r.URL.Query())
	if err != nil {
		return badRequestError(err.Error())
	}
	if from != nil {
		query = query.Where(ordersTable+".created_at >= ?", from)
	}
	if to != nil {
		query.Where(ordersTable+".created_at <= ?", to)
	}

	rows, err := query.Rows()
	if err != nil {
		return internalServerError("Database error").WithInternalError(err)
	}
	defer rows.Close()
	result := []*productsRow{}
	for rows.Next() {
		row := &productsRow{}
		err = rows.Scan(&row.Sku, &row.Path, &row.Total, &row.Currency)
		if err != nil {
			return internalServerError("Database error").WithInternalError(err)
		}
		result = append(result, row)
	}

	return sendJSON(w, http.StatusOK, result)
}
