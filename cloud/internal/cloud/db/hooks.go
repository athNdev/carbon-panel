package db

import "gorm.io/gorm"

// BeforeCreate hooks generating string UUID primary keys. Org and NodeType
// are covered here too: operator-supplied NodeType ids (e.g. "small") are
// preserved, everything else gets a UUID when empty.
func (m *Member) BeforeCreate(*gorm.DB) error        { setTenantID(&m.TenantBase); return nil }
func (k *ApiKey) BeforeCreate(*gorm.DB) error        { setTenantID(&k.TenantBase); return nil }
func (t *JoinToken) BeforeCreate(*gorm.DB) error     { setTenantID(&t.TenantBase); return nil }
func (n *Node) BeforeCreate(*gorm.DB) error          { setTenantID(&n.TenantBase); return nil }
func (p *Provision) BeforeCreate(*gorm.DB) error     { setTenantID(&p.TenantBase); return nil }
func (w *Workload) BeforeCreate(*gorm.DB) error      { setTenantID(&w.TenantBase); return nil }
func (e *WorkloadEvent) BeforeCreate(*gorm.DB) error { setTenantID(&e.TenantBase); return nil }
func (r *RoleBinding) BeforeCreate(*gorm.DB) error   { setTenantID(&r.TenantBase); return nil }
func (b *WorkloadBackup) BeforeCreate(*gorm.DB) error { setTenantID(&b.TenantBase); return nil }

func (e *AuditEvent) BeforeCreate(*gorm.DB) error {
	if e.ID == "" {
		e.ID = newID()
	}
	return nil
}

func setTenantID(b *TenantBase) {
	if b.ID == "" {
		b.ID = newID()
	}
}
