package notify

import (
	"context"
	"encoding/json"
	"time"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/audit"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type dbCtxKey struct{}

const (
	webhookPending  = "pending"
	webhookSent     = "sent"
	webhookFailed   = "failed"
	maxWebhookTries = 5
)

// ContextWithDB attaches a GORM transaction so enqueue joins the caller's commit.
func ContextWithDB(ctx context.Context, tx *gorm.DB) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, dbCtxKey{}, tx)
}

func dbFromContext(ctx context.Context) *gorm.DB {
	if ctx == nil {
		return nil
	}
	tx, _ := ctx.Value(dbCtxKey{}).(*gorm.DB)
	return tx
}

// UseDB persists webhook events instead of calling tenant URLs inside the RPC.
func (d *Dispatcher) UseDB(store *gorm.DB) {
	d.mu.Lock()
	d.db = store
	d.mu.Unlock()
}

func (d *Dispatcher) durable(ctx context.Context) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	// A transaction in ctx is only honored after UseDB. Callers without a
	// store keep the in-process HTTP dispatch used by unit tests.
	return d.db != nil
}

func (d *Dispatcher) gorm(ctx context.Context) *gorm.DB {
	if tx := dbFromContext(ctx); tx != nil {
		return tx
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.db
}

func (d *Dispatcher) enqueueWebhook(ctx context.Context, payload WebhookPayload) {
	gdb := d.gorm(ctx)
	if gdb == nil {
		return
	}
	payload = redactWebhook(payload)
	if payload.EventID == "" {
		payload.EventID = uuid.NewString()
	}
	if payload.Timestamp == 0 {
		payload.Timestamp = time.Now().Unix()
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		_ = gdb.AddError(err)
		return
	}
	row := &db.WebhookEvent{
		TenantBase: db.TenantBase{ID: payload.EventID, OrgID: payload.OrgID},
		EventType:  payload.EventType,
		Payload:    string(raw),
		Status:     webhookPending,
	}
	if err := gdb.WithContext(ctx).Create(row).Error; err != nil {
		_ = gdb.AddError(err)
	}
}

func redactWebhook(payload WebhookPayload) WebhookPayload {
	switch data := payload.Data.(type) {
	case map[string]any:
		payload.Data = audit.Redact(audit.Event{Detail: data}).Detail
	}
	return payload
}

// Relay delivers pending webhook rows. Delivery is at-least-once: a crash
// after HTTP success and before the status update sends the event again.
func (d *Dispatcher) Relay(ctx context.Context, maxBatch int) (int, error) {
	d.mu.RLock()
	gdb := d.db
	d.mu.RUnlock()
	if gdb == nil {
		return 0, nil
	}
	if maxBatch <= 0 {
		maxBatch = 50
	}
	now := time.Now().UTC()
	var rows []db.WebhookEvent
	err := gdb.WithContext(ctx).
		Where("status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)", webhookPending, now).
		Order("created_at asc").
		Limit(maxBatch).
		Find(&rows).Error
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, row := range rows {
		var payload WebhookPayload
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			_ = d.failWebhook(ctx, gdb, &row, err.Error())
			continue
		}
		attempts := d.DispatchWithRetries(ctx, payload, 2, 20*time.Millisecond)
		if len(attempts) == 0 {
			continue
		}
		ok := true
		var lastErr string
		for _, attempt := range attempts {
			ok = ok && attempt.Success
			if attempt.Error != "" {
				lastErr = attempt.Error
			}
			_ = gdb.WithContext(ctx).Create(&db.WebhookAttempt{
				OrgID:       row.OrgID,
				EventID:     row.ID,
				StatusCode:  attempt.StatusCode,
				Error:       attempt.Error,
				Success:     attempt.Success,
				AttemptedAt: attempt.AttemptedAt,
			}).Error
		}
		if ok {
			if err := gdb.WithContext(ctx).Model(&db.WebhookEvent{}).Where("id = ?", row.ID).
				Updates(map[string]any{"status": webhookSent, "last_error": ""}).Error; err != nil {
				return sent, err
			}
			sent++
			continue
		}
		row.Attempts++
		if err := d.scheduleWebhook(ctx, gdb, &row, lastErr); err != nil {
			return sent, err
		}
	}
	return sent, nil
}

func (d *Dispatcher) scheduleWebhook(ctx context.Context, gdb *gorm.DB, row *db.WebhookEvent, lastErr string) error {
	status := webhookPending
	if row.Attempts >= maxWebhookTries {
		status = webhookFailed
	}
	next := time.Now().UTC().Add(time.Duration(row.Attempts) * time.Second)
	return gdb.WithContext(ctx).Model(&db.WebhookEvent{}).Where("id = ?", row.ID).Updates(map[string]any{
		"status":          status,
		"attempts":        row.Attempts,
		"last_error":      lastErr,
		"next_attempt_at": next,
	}).Error
}

func (d *Dispatcher) failWebhook(ctx context.Context, gdb *gorm.DB, row *db.WebhookEvent, lastErr string) error {
	row.Attempts = maxWebhookTries
	return d.scheduleWebhook(ctx, gdb, row, lastErr)
}

// UseDB persists the email queue.
func (o *Outbox) UseDB(store *gorm.DB) {
	o.mu.Lock()
	o.db = store
	o.mu.Unlock()
}

func (o *Outbox) durable(ctx context.Context) bool {
	if dbFromContext(ctx) != nil {
		return true
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.db != nil
}

func (o *Outbox) gorm(ctx context.Context) *gorm.DB {
	if tx := dbFromContext(ctx); tx != nil {
		return tx
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.db
}

func (o *Outbox) enqueueEmail(ctx context.Context, orgID, recipient, subject, bodyText, bodyHTML string) *EmailMessage {
	gdb := o.gorm(ctx)
	msg := &EmailMessage{
		ID:        uuid.NewString(),
		OrgID:     orgID,
		Recipient: recipient,
		Subject:   subject,
		BodyText:  bodyText,
		BodyHTML:  bodyHTML,
		Status:    EmailStatusPending,
		CreatedAt: time.Now().UTC(),
	}
	row := &db.EmailOutbox{
		TenantBase: db.TenantBase{ID: msg.ID, OrgID: orgID},
		Recipient:  recipient,
		Subject:    subject,
		BodyText:   bodyText,
		BodyHTML:   bodyHTML,
		Status:     string(EmailStatusPending),
	}
	if err := gdb.WithContext(ctx).Create(row).Error; err != nil {
		_ = gdb.AddError(err)
	}
	return msg
}

func (o *Outbox) flushEmail(ctx context.Context, maxBatch int) (int, error) {
	o.mu.Lock()
	gdb := o.db
	o.mu.Unlock()
	if gdb == nil {
		return 0, nil
	}
	if maxBatch <= 0 {
		maxBatch = 50
	}
	var rows []db.EmailOutbox
	err := gdb.WithContext(ctx).Where("status = ?", string(EmailStatusPending)).
		Order("created_at asc").Limit(maxBatch).Find(&rows).Error
	if err != nil {
		return 0, err
	}
	sent := 0
	var lastErr error
	for i := range rows {
		row := &rows[i]
		msg := emailFromRow(row)
		msg.Attempts = row.Attempts + 1
		err := o.sender.SendEmail(ctx, msg)
		updates := map[string]any{"attempts": msg.Attempts}
		if err != nil {
			lastErr = err
			updates["last_error"] = err.Error()
			if msg.Attempts >= 3 {
				updates["status"] = string(EmailStatusFailed)
			} else {
				updates["status"] = string(EmailStatusPending)
			}
		} else {
			now := time.Now().UTC()
			updates["status"] = string(EmailStatusSent)
			updates["sent_at"] = now
			updates["last_error"] = ""
			sent++
		}
		if uerr := gdb.WithContext(ctx).Model(&db.EmailOutbox{}).Where("id = ?", row.ID).Updates(updates).Error; uerr != nil {
			return sent, uerr
		}
	}
	return sent, lastErr
}

func (o *Outbox) emailHistory() []*EmailMessage {
	o.mu.Lock()
	gdb := o.db
	o.mu.Unlock()
	if gdb == nil {
		return nil
	}
	var rows []db.EmailOutbox
	_ = gdb.Where("status <> ?", string(EmailStatusPending)).Order("created_at asc").Find(&rows).Error
	out := make([]*EmailMessage, 0, len(rows))
	for i := range rows {
		out = append(out, emailFromRow(&rows[i]))
	}
	return out
}

func emailFromRow(row *db.EmailOutbox) *EmailMessage {
	return &EmailMessage{
		ID:        row.ID,
		OrgID:     row.OrgID,
		Recipient: row.Recipient,
		Subject:   row.Subject,
		BodyText:  row.BodyText,
		BodyHTML:  row.BodyHTML,
		Status:    EmailStatus(row.Status),
		Attempts:  row.Attempts,
		LastError: row.LastError,
		CreatedAt: row.CreatedAt,
		SentAt:    row.SentAt,
	}
}
