package merchants

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BootstrapContractVersion is the version of the subject-oriented bootstrap
// contract consumed by actor services for safe frontend evolution.
const BootstrapContractVersion = "1"

// PlanStateNoPlanModel is the explicit plan state used while the platform has
// no plan/subscription model. It is honest-empty: a plan is never fabricated.
const PlanStateNoPlanModel = "NO_PLAN_MODEL"

const (
	maxBootstrapMerchants = 100
	maxBootstrapStores    = 500
)

// MerchantWorkspaceMembership is the membership facet exposed by the bootstrap.
type MerchantWorkspaceMembership struct {
	ID          uuid.UUID        `json:"id"`
	Status      MembershipStatus `json:"status"`
	Permissions []string         `json:"permissions"`
}

// WorkspaceCapability carries one capability state.
type WorkspaceCapability struct {
	Status CapabilityStatus `json:"status"`
}

// IsActive reports whether the capability authorizes its module.
func (c *WorkspaceCapability) IsActive() bool {
	return c != nil && c.Status == CapabilityStatusActive
}

// WorkspaceCapabilities is the per-workspace Retail/Supply capability state.
type WorkspaceCapabilities struct {
	Retail *WorkspaceCapability `json:"retail"`
	Supply *WorkspaceCapability `json:"supply"`
}

// WorkspaceStoreSummary is one authorized store inside the workspace.
type WorkspaceStoreSummary struct {
	ID         uuid.UUID `json:"id"`
	Code       string    `json:"code"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	MarketCode string    `json:"market_code"`
}

// WorkspacePlanSummary is the honest plan state of the workspace.
type WorkspacePlanSummary struct {
	State string `json:"state"`
}

// WorkspacePendingActions counts actions owed by the Merchant, derived only
// from persisted state.
type WorkspacePendingActions struct {
	OpenReviewCases      int `json:"open_review_cases"`
	UnhealthyConnections int `json:"unhealthy_connections"`
}

// MerchantWorkspace is one Merchant workspace the subject holds a membership in.
// For memberships that are not active, or a Merchant that is not active, the
// workspace exposes only identity/status facts: no capability, store, plan, or
// pending-action details are released.
type MerchantWorkspace struct {
	MerchantID     uuid.UUID                   `json:"merchant_id"`
	MerchantCode   string                      `json:"merchant_code"`
	LegalName      string                      `json:"legal_name"`
	MerchantStatus MerchantStatus              `json:"merchant_status"`
	Membership     MerchantWorkspaceMembership `json:"membership"`
	Capabilities   *WorkspaceCapabilities      `json:"capabilities,omitempty"`
	Stores         []WorkspaceStoreSummary     `json:"stores"`
	PlanSummary    *WorkspacePlanSummary       `json:"plan_summary,omitempty"`
	PendingActions *WorkspacePendingActions    `json:"pending_actions,omitempty"`
}

// BootstrapMeta carries contract/version metadata for safe frontend evolution.
type BootstrapMeta struct {
	ContractVersion string    `json:"contract_version"`
	GeneratedAt     time.Time `json:"generated_at"`
}

// MerchantBootstrap is the subject-oriented Merchant workspace resolution.
// Workspaces MAY be empty: a principal without canonical membership has no
// workspace, and no default selection is ever applied.
type MerchantBootstrap struct {
	Subject    string              `json:"subject"`
	Workspaces []MerchantWorkspace `json:"workspaces"`
	Meta       BootstrapMeta       `json:"meta"`
}

// BootstrapService aggregates the subject-oriented bootstrap read path over the
// canonical Merchant tables. It is read-only and performs a bounded,
// deterministic set of queries (no per-store/per-capability N+1).
type BootstrapService struct {
	pool *pgxpool.Pool
	repo *PostgresRepository
}

func NewBootstrapService(pool *pgxpool.Pool, repo *PostgresRepository) *BootstrapService {
	return &BootstrapService{pool: pool, repo: repo}
}

type merchantRow struct {
	id     uuid.UUID
	code   string
	name   string
	status MerchantStatus
}

// Build resolves the authenticated principal to all Merchant workspaces where
// the principal holds a canonical membership. Merchant ownership is sourced
// exclusively from explicit membership records.
func (s *BootstrapService) Build(ctx context.Context, subject string) (*MerchantBootstrap, error) {
	bs := &MerchantBootstrap{
		Subject:    subject,
		Workspaces: []MerchantWorkspace{},
		Meta: BootstrapMeta{
			ContractVersion: BootstrapContractVersion,
			GeneratedAt:     time.Now().UTC(),
		},
	}

	mems, err := s.repo.ListMembershipsBySubject(ctx, subject)
	if err != nil {
		return nil, err
	}
	if len(mems) == 0 {
		return bs, nil
	}

	merchantIDs := make([]uuid.UUID, 0, len(mems))
	seenMerchant := make(map[uuid.UUID]bool, len(mems))
	for _, mem := range mems {
		if !seenMerchant[mem.MerchantID] {
			seenMerchant[mem.MerchantID] = true
			merchantIDs = append(merchantIDs, mem.MerchantID)
		}
	}

	merchByID, err := s.loadMerchants(ctx, merchantIDs)
	if err != nil {
		return nil, err
	}
	capsByMerchant, err := s.loadCapabilities(ctx, merchantIDs)
	if err != nil {
		return nil, err
	}
	storesByMerchant, err := s.loadStoreSummaries(ctx, merchantIDs)
	if err != nil {
		return nil, err
	}
	reviewCasesByMerchant, err := s.loadOpenReviewCaseCounts(ctx, merchantIDs)
	if err != nil {
		return nil, err
	}
	unhealthyByMerchant, err := s.loadUnhealthyConnectionCounts(ctx, merchantIDs)
	if err != nil {
		return nil, err
	}

	for _, mem := range mems {
		merchant, ok := merchByID[mem.MerchantID]
		if !ok {
			// The membership references a merchant that no longer resolves;
			// skip rather than fabricate identity facts.
			continue
		}
		ws := MerchantWorkspace{
			MerchantID:     merchant.id,
			MerchantCode:   merchant.code,
			LegalName:      merchant.name,
			MerchantStatus: merchant.status,
			Membership: MerchantWorkspaceMembership{
				ID:          mem.ID,
				Status:      mem.Status,
				Permissions: mem.Permissions,
			},
			Stores: []WorkspaceStoreSummary{},
		}

		if merchant.status == MerchantStatusActive && mem.Status == MembershipStatusActive {
			ws.Capabilities = capsByMerchant[merchant.id]
			ws.Stores = storesByMerchant[merchant.id]
			ws.PlanSummary = &WorkspacePlanSummary{State: PlanStateNoPlanModel}
			ws.PendingActions = &WorkspacePendingActions{
				OpenReviewCases:      reviewCasesByMerchant[merchant.id],
				UnhealthyConnections: unhealthyByMerchant[merchant.id],
			}
		}
		bs.Workspaces = append(bs.Workspaces, ws)
	}
	return bs, nil
}

func (s *BootstrapService) loadMerchants(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]merchantRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, code, legal_name, status FROM merchants WHERE id = ANY($1) LIMIT $2
	`, ids, maxBootstrapMerchants)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := make(map[uuid.UUID]merchantRow, len(ids))
	for rows.Next() {
		var m merchantRow
		var statusStr string
		if err := rows.Scan(&m.id, &m.code, &m.name, &statusStr); err != nil {
			return nil, err
		}
		m.status = MerchantStatus(statusStr)
		byID[m.id] = m
	}
	return byID, rows.Err()
}

func (s *BootstrapService) loadCapabilities(ctx context.Context, merchantIDs []uuid.UUID) (map[uuid.UUID]*WorkspaceCapabilities, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT merchant_id, capability_type, status FROM merchant_capabilities WHERE merchant_id = ANY($1)
	`, merchantIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byMerchant := make(map[uuid.UUID]*WorkspaceCapabilities, len(merchantIDs))
	for _, id := range merchantIDs {
		byMerchant[id] = &WorkspaceCapabilities{
			Retail: &WorkspaceCapability{Status: CapabilityStatusInactive},
			Supply: &WorkspaceCapability{Status: CapabilityStatusInactive},
		}
	}
	for rows.Next() {
		var merchantID uuid.UUID
		var capTypeStr, statusStr string
		if err := rows.Scan(&merchantID, &capTypeStr, &statusStr); err != nil {
			return nil, err
		}
		caps, ok := byMerchant[merchantID]
		if !ok {
			continue
		}
		status := CapabilityStatus(statusStr)
		switch CapabilityType(capTypeStr) {
		case CapabilityTypeRetail:
			caps.Retail = &WorkspaceCapability{Status: status}
		case CapabilityTypeSupply:
			caps.Supply = &WorkspaceCapability{Status: status}
		}
	}
	return byMerchant, rows.Err()
}

func (s *BootstrapService) loadStoreSummaries(ctx context.Context, merchantIDs []uuid.UUID) (map[uuid.UUID][]WorkspaceStoreSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sel.merchant_id, st.id, st.code, st.name, st.status, st.market_code
		FROM stores st
		JOIN sellers sel ON sel.id = st.seller_id
		WHERE sel.merchant_id = ANY($1)
		ORDER BY st.created_at, st.id
		LIMIT $2
	`, merchantIDs, maxBootstrapStores)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byMerchant := make(map[uuid.UUID][]WorkspaceStoreSummary, len(merchantIDs))
	for rows.Next() {
		var merchantID uuid.UUID
		var st WorkspaceStoreSummary
		if err := rows.Scan(&merchantID, &st.ID, &st.Code, &st.Name, &st.Status, &st.MarketCode); err != nil {
			return nil, err
		}
		byMerchant[merchantID] = append(byMerchant[merchantID], st)
	}
	return byMerchant, rows.Err()
}

func (s *BootstrapService) loadOpenReviewCaseCounts(ctx context.Context, merchantIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT merchant_id, COUNT(*) FROM merchant_integration_review_cases
		WHERE merchant_id = ANY($1) AND status = 'OPEN'
		GROUP BY merchant_id
	`, merchantIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[uuid.UUID]int, len(merchantIDs))
	for rows.Next() {
		var id uuid.UUID
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		counts[id] = n
	}
	return counts, rows.Err()
}

// loadUnhealthyConnectionCounts counts connections whose persisted health state
// is actionable: a health_status other than 'healthy' or lifecycle status
// ERROR. Deliberate lifecycle states (DRAFT/AUTHORIZING/CONFIGURING/PAUSED/
// REVOKED/RETIRED) are not health failures.
func (s *BootstrapService) loadUnhealthyConnectionCounts(ctx context.Context, merchantIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT merchant_id, COUNT(*) FROM merchant_integration_connections
		WHERE merchant_id = ANY($1) AND (health_status <> 'healthy' OR status = 'ERROR')
		GROUP BY merchant_id
	`, merchantIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[uuid.UUID]int, len(merchantIDs))
	for rows.Next() {
		var id uuid.UUID
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		counts[id] = n
	}
	return counts, rows.Err()
}
