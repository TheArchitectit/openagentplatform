package hitl

// ============================================================
// Store — persistence interface for approvals and audit.
// ============================================================

// Store abstracts approval persistence. Implementations may use
// a database, file system, or in-memory map.
type Store interface {
	// SaveApproval persists an approval request (create or update).
	SaveApproval(req *ApprovalRequest) error

	// GetApproval retrieves an approval by ID.
	GetApproval(id string) (*ApprovalRequest, error)

	// ListApprovalsByStatus returns approvals matching the status.
	ListApprovalsByStatus(status ApprovalStatus) ([]*ApprovalRequest, error)

	// AppendAudit adds an audit entry.
	AppendAudit(entry AuditEntry) error

	// GetAuditLog returns the audit trail for an approval.
	GetAuditLog(approvalID string) ([]AuditEntry, error)
}

// ============================================================
// MemStore — in-memory store for testing.
// ============================================================

// MemStore is a thread-safe in-memory Store for tests.
type MemStore struct {
	approvals map[string]*ApprovalRequest
	auditLog  []AuditEntry
}

// NewMemStore creates an in-memory store.
func NewMemStore() *MemStore {
	return &MemStore{
		approvals: make(map[string]*ApprovalRequest),
	}
}

func (s *MemStore) SaveApproval(req *ApprovalRequest) error {
	s.approvals[req.ID] = req
	return nil
}

func (s *MemStore) GetApproval(id string) (*ApprovalRequest, error) {
	req, ok := s.approvals[id]
	if !ok {
		return nil, ErrApprovalNotFound
	}
	return req, nil
}

func (s *MemStore) ListApprovalsByStatus(status ApprovalStatus) ([]*ApprovalRequest, error) {
	var result []*ApprovalRequest
	for _, req := range s.approvals {
		if req.Status == status {
			result = append(result, req)
		}
	}
	return result, nil
}

func (s *MemStore) AppendAudit(entry AuditEntry) error {
	s.auditLog = append(s.auditLog, entry)
	return nil
}

func (s *MemStore) GetAuditLog(approvalID string) ([]AuditEntry, error) {
	var entries []AuditEntry
	for _, e := range s.auditLog {
		if e.ApprovalID == approvalID {
			entries = append(entries, e)
		}
	}
	return entries, nil
}
