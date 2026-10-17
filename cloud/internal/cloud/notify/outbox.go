// Package notify handles outbound communications including email outbox delivery
// and cryptographically signed HTTP webhooks.
package notify

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// EmailStatus represents delivery state of an outbox message.
type EmailStatus string

const (
	EmailStatusPending EmailStatus = "pending"
	EmailStatusSent    EmailStatus = "sent"
	EmailStatusFailed  EmailStatus = "failed"
)

// EmailMessage contains the details of an outbound email.
type EmailMessage struct {
	ID        string      `json:"id"`
	OrgID     string      `json:"org_id"`
	Recipient string      `json:"recipient"`
	Subject   string      `json:"subject"`
	BodyText  string      `json:"body_text"`
	BodyHTML  string      `json:"body_html,omitempty"`
	Status    EmailStatus `json:"status"`
	Attempts  int         `json:"attempts"`
	LastError string      `json:"last_error,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
	SentAt    *time.Time  `json:"sent_at,omitempty"`
}

// EmailSender delivers an email to the external SMTP or transactional email provider.
type EmailSender interface {
	SendEmail(ctx context.Context, msg *EmailMessage) error
}

// FakeEmailSender records delivered emails in-memory for testing.
type FakeEmailSender struct {
	mu       sync.Mutex
	Sent     []*EmailMessage
	FailNext bool
}

// NewFakeEmailSender initializes a test email sender.
func NewFakeEmailSender() *FakeEmailSender {
	return &FakeEmailSender{}
}

func (f *FakeEmailSender) SendEmail(ctx context.Context, msg *EmailMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.FailNext {
		f.FailNext = false
		return errors.New("simulated SMTP delivery failure")
	}

	copied := *msg
	now := time.Now()
	copied.SentAt = &now
	f.Sent = append(f.Sent, &copied)
	return nil
}

// Outbox manages queued outbound emails.
type Outbox struct {
	mu      sync.Mutex
	sender  EmailSender
	queue   []*EmailMessage
	history []*EmailMessage
	counter int
}

// NewOutbox initializes an email outbox.
func NewOutbox(sender EmailSender) *Outbox {
	if sender == nil {
		sender = NewFakeEmailSender()
	}
	return &Outbox{
		sender: sender,
	}
}

// Enqueue adds an email to the pending outbox queue.
func (o *Outbox) Enqueue(orgID, recipient, subject, bodyText, bodyHTML string) *EmailMessage {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.counter++
	msg := &EmailMessage{
		ID:        fmt.Sprintf("mail_%d", o.counter),
		OrgID:     orgID,
		Recipient: recipient,
		Subject:   subject,
		BodyText:  bodyText,
		BodyHTML:  bodyHTML,
		Status:    EmailStatusPending,
		CreatedAt: time.Now(),
	}
	o.queue = append(o.queue, msg)
	return msg
}

// PendingCount returns the number of unsent messages.
func (o *Outbox) PendingCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.queue)
}

// Flush processes all pending emails up to maxBatch, returning the count of successfully sent emails.
func (o *Outbox) Flush(ctx context.Context, maxBatch int) (int, error) {
	o.mu.Lock()
	if len(o.queue) == 0 {
		o.mu.Unlock()
		return 0, nil
	}

	batchSize := len(o.queue)
	if maxBatch > 0 && batchSize > maxBatch {
		batchSize = maxBatch
	}

	batch := o.queue[:batchSize]
	o.queue = o.queue[batchSize:]
	o.mu.Unlock()

	sentCount := 0
	var lastErr error

	for _, msg := range batch {
		msg.Attempts++
		err := o.sender.SendEmail(ctx, msg)
		o.mu.Lock()
		if err != nil {
			msg.Status = EmailStatusFailed
			msg.LastError = err.Error()
			lastErr = err
			// If fewer than 3 attempts, re-queue for next flush
			if msg.Attempts < 3 {
				msg.Status = EmailStatusPending
				o.queue = append(o.queue, msg)
			} else {
				o.history = append(o.history, msg)
			}
		} else {
			msg.Status = EmailStatusSent
			now := time.Now()
			msg.SentAt = &now
			sentCount++
			o.history = append(o.history, msg)
		}
		o.mu.Unlock()
	}

	return sentCount, lastErr
}

// History returns a copy of completed (sent or permanently failed) messages.
func (o *Outbox) History() []*EmailMessage {
	o.mu.Lock()
	defer o.mu.Unlock()

	res := make([]*EmailMessage, len(o.history))
	copy(res, o.history)
	return res
}
