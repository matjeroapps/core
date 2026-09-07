package coreapi

import (
	"net/http"

	"github.com/matjeroapps/core/internal/suppliers"
	"github.com/matjeroapps/core/modules/commerce"
	"github.com/matjeroapps/core/packages/httpx"
)

// handleGetSupplierRetailCapability retrieves the explicit 1:1 retail link and seller profile for a supplier.
func (s *server) handleGetSupplierRetailCapability(w http.ResponseWriter, r *http.Request) {
	subject, supplierID, ok := s.authorizeSupplierSubject(w, r)
	if !ok {
		return
	}
	seller, err := s.deps.Commerce.RequireSupplierRetailAccess(r.Context(), subject, supplierID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	affiliation, err := s.deps.Repo.GetSupplierSellerAffiliationBySupplierID(r.Context(), supplierID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, SupplierRetailCapabilityResponse{
		Affiliation: affiliation,
		Seller:      seller,
	})
}

// handleCreateSupplierRetailCapability provisions a retail capability for a supplier using the atomic supplier domain service.
func (s *server) handleCreateSupplierRetailCapability(w http.ResponseWriter, r *http.Request) {
	subject, supplierID, ok := s.authorizeSupplierSubject(w, r)
	if !ok {
		return
	}
	var body SupplierRetailCapabilityRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	sellerProfile, affiliation, err := suppliers.ProvisionRetailCapabilityTx(
		r.Context(),
		s.deps.Repo.Pool(),
		supplierID,
		subject,
		suppliers.ProvisionRetailParams{
			Code:     body.Code,
			Name:     body.Name,
			Settings: body.Settings,
		},
	)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, SupplierRetailCapabilityResponse{
		Affiliation: commerce.SupplierSellerAffiliation{
			SupplierID: affiliation.SupplierID,
			SellerID:   affiliation.SellerID,
			CreatedAt:  affiliation.CreatedAt,
		},
		Seller: commerce.Seller{
			ID:        sellerProfile.ID,
			Code:      sellerProfile.Code,
			Name:      sellerProfile.Name,
			Status:    sellerProfile.Status,
			CreatedAt: sellerProfile.CreatedAt,
			UpdatedAt: sellerProfile.UpdatedAt,
		},
	})
}
