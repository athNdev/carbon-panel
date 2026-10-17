package billing

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// InvoiceStatus represents the lifecycle state of an invoice.
type InvoiceStatus string

const (
	InvoiceStatusDraft InvoiceStatus = "draft"
	InvoiceStatusOpen  InvoiceStatus = "open"
	InvoiceStatusPaid  InvoiceStatus = "paid"
	InvoiceStatusVoid  InvoiceStatus = "void"
)

// Invoice represents a billing charge issued to an organization.
type Invoice struct {
	ID          string        `json:"id"`
	OrgID       string        `json:"org_id"`
	CustomerID  string        `json:"customer_id"`
	AmountCents int64         `json:"amount_cents"`
	Currency    string        `json:"currency"`
	Description string        `json:"description"`
	Status      InvoiceStatus `json:"status"`
	CreatedAt   time.Time     `json:"created_at"`
	PaidAt      *time.Time    `json:"paid_at,omitempty"`
}

// Invoicer provides external billing integrations (e.g. Stripe, Polar, Lago, or Fake/Noop).
type Invoicer interface {
	// CreateCustomer registers an organization with the billing provider.
	CreateCustomer(ctx context.Context, orgID, email, name string) (customerID string, err error)
	// SyncSubscription updates the tenant plan tier in the provider.
	SyncSubscription(ctx context.Context, orgID, planID string) error
	// IssueInvoice creates and finalizes a charge invoice for the tenant.
	IssueInvoice(ctx context.Context, orgID string, amountCents int64, description string) (*Invoice, error)
	// ListInvoices returns all invoices issued to an organization.
	ListInvoices(ctx context.Context, orgID string) ([]*Invoice, error)
}

// FakeInvoicer is an in-memory implementation for tests and dev environments.
type FakeInvoicer struct {
	mu            sync.Mutex
	customers     map[string]string // orgID -> customerID
	subscriptions map[string]string // orgID -> planID
	invoices      map[string][]*Invoice
	counter       int
}

// NewFakeInvoicer initializes a new fake invoicing adapter.
func NewFakeInvoicer() *FakeInvoicer {
	return &FakeInvoicer{
		customers:     make(map[string]string),
		subscriptions: make(map[string]string),
		invoices:      make(map[string][]*Invoice),
	}
}

func (f *FakeInvoicer) CreateCustomer(ctx context.Context, orgID, email, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	custID := fmt.Sprintf("cust_%s", orgID)
	f.customers[orgID] = custID
	return custID, nil
}

func (f *FakeInvoicer) SyncSubscription(ctx context.Context, orgID, planID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.subscriptions[orgID] = planID
	return nil
}

func (f *FakeInvoicer) IssueInvoice(ctx context.Context, orgID string, amountCents int64, description string) (*Invoice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	custID := f.customers[orgID]
	if custID == "" {
		custID = fmt.Sprintf("cust_%s", orgID)
		f.customers[orgID] = custID
	}

	f.counter++
	now := time.Now()
	paid := now
	inv := &Invoice{
		ID:          fmt.Sprintf("inv_test_%d", f.counter),
		OrgID:       orgID,
		CustomerID:  custID,
		AmountCents: amountCents,
		Currency:    "USD",
		Description: description,
		Status:      InvoiceStatusPaid,
		CreatedAt:   now,
		PaidAt:      &paid,
	}

	f.invoices[orgID] = append(f.invoices[orgID], inv)
	return inv, nil
}

func (f *FakeInvoicer) ListInvoices(ctx context.Context, orgID string) ([]*Invoice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]*Invoice(nil), f.invoices[orgID]...), nil
}

// NoopInvoicer is a disabled billing adapter for self-hosted or open-source deployments.
type NoopInvoicer struct{}

func (n *NoopInvoicer) CreateCustomer(context.Context, string, string, string) (string, error) {
	return "disabled", nil
}

func (n *NoopInvoicer) SyncSubscription(context.Context, string, string) error {
	return nil
}

func (n *NoopInvoicer) IssueInvoice(context.Context, string, int64, string) (*Invoice, error) {
	return nil, errors.New("invoicing is disabled in this deployment")
}

func (n *NoopInvoicer) ListInvoices(context.Context, string) ([]*Invoice, error) {
	return nil, nil
}
