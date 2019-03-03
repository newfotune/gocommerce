package paypal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"gocommerce/models"
	"github.com/pariz/gountries"

	paypalsdk "github.com/netlify/PayPal-Go-SDK"
	"gocommerce/conf"
	gcontext "gocommerce/context"
	"gocommerce/payments"
	"github.com/pborman/uuid"
	"github.com/pkg/errors"
)

type paypalPaymentProvider struct {
	client       *paypalsdk.Client
	profile      *paypalsdk.WebProfile
	profileMutex sync.Mutex
}

type paypalBodyParams struct {
	PaypalID     string `json:"paypal_payment_id"`
	PaypalUserID string `json:"paypal_user_id"`
}

// Config contains PayPal-specific configuration for payment providers.
type Config struct {
	ClientID string `mapstructure:"client_id" json:"client_id"`
	Secret   string `mapstructure:"secret" json:"secret"`
	Env      string `mapstructure:"env" json:"env"`
}

// NewPaymentProvider creates a new PayPal payment provider using the provided configuration.
func NewPaymentProvider(config Config) (payments.Provider, error) {
	var paypal *paypalsdk.Client
	if config.ClientID == "" || config.Secret == "" {
		return nil, errors.New("missing PayPal client_id and/or secret")
	}
	var ppEnv string
	if config.Env == "production" {
		ppEnv = paypalsdk.APIBaseLive
	} else if config.Env == "sandbox" {
		ppEnv = paypalsdk.APIBaseSandBox
	} else {
		// used for testing
		ppEnv = config.Env
	}

	paypal, err := paypalsdk.NewClient(
		config.ClientID,
		config.Secret,
		ppEnv,
	)
	if err != nil {
		return nil, errors.Wrap(err, "Error configuring paypal")
	}
	_, err = paypal.GetAccessToken()
	if err != nil {
		return nil, errors.Wrap(err, "Error authorizing with paypal")
	}

	return &paypalPaymentProvider{
		client: paypal,
	}, nil
}

func (p *paypalPaymentProvider) Name() string {
	return payments.PayPalProvider
}

func (p *paypalPaymentProvider) NewCharger(ctx context.Context, r *http.Request) (payments.Charger, error) {
	var bp paypalBodyParams
	bod, err := r.GetBody()
	if err != nil {
		return nil, err
	}
	err = json.NewDecoder(bod).Decode(&bp)
	if err != nil {
		return nil, err
	}
	if bp.PaypalID == "" || bp.PaypalUserID == "" {
		return nil, errors.New("Payments requires a paypal_payment_id and paypal_user_id pair")
	}

	return func(amount uint64, currency string, order *models.Order, invoiceNumber int64) (string, error) {
		return p.charge(bp.PaypalID, bp.PaypalUserID, amount, currency, order, invoiceNumber)
	}, nil
}

func prepareItemsFromOrder(order *models.Order) []paypalsdk.Item {
	items := []paypalsdk.Item{}
	for _, lineItem := range order.LineItems {
		item := paypalsdk.Item{
			Quantity:    int(lineItem.GetQuantity()),
			Name:        lineItem.Title,
			Price:       formatAmount(lineItem.PriceInLowestUnit()),
			Currency:    order.Currency,
			SKU:         lineItem.ProductSku(),
			Description: lineItem.Description,
		}
		if lineItem.FixedVAT() > 0 {
			item.Tax = fmt.Sprintf("%d%%", lineItem.FixedVAT())
		}
		items = append(items, item)
	}
	return items
}

func prepareShippingAddress(addr models.Address) *paypalsdk.ShippingAddress {
	countryQuery := gountries.New()

	country, err := countryQuery.FindCountryByName(strings.ToLower(addr.Country))
	if err != nil {
		return nil
	}

	return &paypalsdk.ShippingAddress{
		RecipientName: addr.Name,
		Line1:         addr.Address1,
		Line2:         addr.Address2,
		City:          addr.City,
		CountryCode:   country.Codes.Alpha2,
		PostalCode:    addr.Zip,
		State:         addr.State,
	}
}

func (p *paypalPaymentProvider) updatePaymentWithOrder(paymentID string, order *models.Order, invoiceNumber int64) error {
	invoiceNumPatch := paypalsdk.PaymentPatch{
		Operation: "add",
		Path:      "/transactions/0/invoice_number",
		Value:     fmt.Sprintf("%d", invoiceNumber),
	}

	itemList := paypalsdk.ItemList{
		Items: prepareItemsFromOrder(order),
	}
	if a := prepareShippingAddress(order.ShippingAddress); a != nil {
		itemList.ShippingAddress = a
	}
	itemListPatch := paypalsdk.PaymentPatch{
		Operation: "add",
		Path:      "/transactions/0/item_list",
		Value:     &itemList,
	}

	_, err := p.client.PatchPayment(paymentID, []paypalsdk.PaymentPatch{invoiceNumPatch, itemListPatch})
	if err != nil {
		switch e := err.(type) {
		case *paypalsdk.ErrorResponse:
			fmt.Println(e.Details)
		}
	}
	return err
}

func (p *paypalPaymentProvider) charge(paymentID string, userID string, amount uint64, currency string, order *models.Order, invoiceNumber int64) (string, error) {
	payment, err := p.client.GetPayment(paymentID)
	if err != nil {
		return "", err
	}
	if len(payment.Transactions) != 1 {
		return "", fmt.Errorf("The paypal payment must have exactly 1 transaction, had %v", len(payment.Transactions))
	}

	if payment.Transactions[0].Amount == nil {
		return "", fmt.Errorf("No amount in this transaction %v", payment.Transactions[0])
	}

	transactionValue := fmt.Sprintf("%.2f", float64(amount)/100)

	if transactionValue != payment.Transactions[0].Amount.Total || payment.Transactions[0].Amount.Currency != currency {
		return "", fmt.Errorf("The Amount in the transaction doesn't match the amount for the order: %v", payment.Transactions[0].Amount)
	}

	if err := p.updatePaymentWithOrder(paymentID, order, invoiceNumber); err != nil {
		return "", errors.Wrap(err, "Updating the PayPal payment with order details failed")
	}

	executeResult, err := p.client.ExecuteApprovedPayment(paymentID, userID)
	if err != nil {
		return "", err
	}

	return executeResult.ID, nil
}

func (p *paypalPaymentProvider) NewRefunder(ctx context.Context, r *http.Request) (payments.Refunder, error) {
	return p.refund, nil
}

func (p *paypalPaymentProvider) refund(transactionID string, amount uint64, currency string) (string, error) {
	amt := &paypalsdk.Amount{
		Total:    formatAmount(amount),
		Currency: currency,
	}
	ref, err := p.client.RefundSale(transactionID, amt)
	if err != nil {
		return "", err
	}
	return ref.ID, nil
}

func (p *paypalPaymentProvider) NewPreauthorizer(ctx context.Context, r *http.Request) (payments.Preauthorizer, error) {
	config := gcontext.GetConfig(ctx)
	return func(amount uint64, currency string, description string) (*payments.PreauthorizationResult, error) {
		return p.preauthorize(config, amount, currency, description)
	}, nil
}

func (p *paypalPaymentProvider) preauthorize(config *conf.Configuration, amount uint64, currency string, description string) (*payments.PreauthorizationResult, error) {
	profile, err := p.getExperience()
	if err != nil {
		return nil, errors.Wrap(err, "error creating paypal experience")
	}

	redirectURI := config.SiteURL + "/gocommerce/paypal"
	cancelURI := config.SiteURL + "/gocommerce/paypal/cancel"
	paymentResult, err := p.client.CreatePayment(paypalsdk.Payment{
		Intent: "sale",
		Payer: &paypalsdk.Payer{
			PaymentMethod: "paypal",
		},
		ExperienceProfileID: profile.ID,
		Transactions: []paypalsdk.Transaction{paypalsdk.Transaction{
			Amount: &paypalsdk.Amount{
				Total:    formatAmount(amount),
				Currency: currency,
			},
			Description: description,
		}},
		RedirectURLs: &paypalsdk.RedirectURLs{
			ReturnURL: redirectURI,
			CancelURL: cancelURI,
		},
	})

	if err != nil {
		return nil, errors.Wrap(err, "error creating paypal payment")
	}
	return &payments.PreauthorizationResult{
		ID: paymentResult.ID,
	}, nil
}

func (p *paypalPaymentProvider) getExperience() (*paypalsdk.WebProfile, error) {
	p.profileMutex.Lock()
	defer p.profileMutex.Unlock()

	if p.profile != nil {
		return p.profile, nil
	}

	profile, err := p.client.CreateWebProfile(paypalsdk.WebProfile{
		Name:      "gocommerce-" + uuid.NewRandom().String(),
		Temporary: true,
		InputFields: paypalsdk.InputFields{
			NoShipping: 1,
		},
	})

	if err != nil {
		return nil, errors.Wrap(err, "failed creating web profile")
	}

	p.profile = profile
	return profile, nil
}

func formatAmount(amount uint64) string {
	return strconv.FormatFloat(float64(amount)/100, 'f', 2, 64)
}
