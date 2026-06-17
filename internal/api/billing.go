package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/billing"
	"github.com/openagentplatform/openagentplatform/internal/license"
)

// billingCreateCustomerRequest is the JSON body for POST
// /api/v1/billing/create-customer.
type billingCreateCustomerRequest struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

// billingCreateSubscriptionRequest is the JSON body for POST
// /api/v1/billing/create-subscription. Tier must be one of
// community, professional, enterprise.
type billingCreateSubscriptionRequest struct {
	Tier string `json:"tier"`
}

// billingWebhookResponse is the minimal acknowledgement returned to
// Stripe after successful event processing.
type billingWebhookResponse struct {
	Received bool   `json:"received"`
	EventID  string `json:"event_id,omitempty"`
}

// orgFromRequest extracts the OrgID from the authenticated session.
func billingOrgFromRequest(r *http.Request) (string, error) {
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || claims == nil {
		return "", errors.New("unauthorized")
	}
	if claims.OrgID == "" {
		return "", errors.New("org context required")
	}
	return claims.OrgID, nil
}

// handleCreateCustomer provisions a Stripe customer for the caller's
// organisation.
func (s *Server) handleCreateCustomer(w http.ResponseWriter, r *http.Request) {
	orgID, err := billingOrgFromRequest(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if s.BillingService == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "billing_unavailable")
		return
	}
	var req billingCreateCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request_body")
		return
	}
	if req.Email == "" || req.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "email_and_name_required")
		return
	}
	state, err := s.BillingService.CreateCustomer(r.Context(), orgID, req.Email, req.Name)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "create_customer_failed")
		return
	}
	writeJSON(w, http.StatusCreated, state)
}

// handleCreateSubscription starts a Stripe subscription for the
// caller's organisation at the requested tier.
func (s *Server) handleCreateSubscription(w http.ResponseWriter, r *http.Request) {
	orgID, err := billingOrgFromRequest(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if s.BillingService == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "billing_unavailable")
		return
	}
	var req billingCreateSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request_body")
		return
	}
	tier := license.Tier(req.Tier)
	if _, ok := billing.TierCatalog[tier]; !ok {
		writeJSONError(w, http.StatusBadRequest, "unknown_tier")
		return
	}
	state, err := s.BillingService.CreateSubscription(r.Context(), orgID, tier)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "create_subscription_failed")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// handleGetSubscription returns the cached billing state for the org.
func (s *Server) handleGetSubscription(w http.ResponseWriter, r *http.Request) {
	orgID, err := billingOrgFromRequest(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if s.BillingService == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "billing_unavailable")
		return
	}
	state, err := s.BillingService.GetSubscription(r.Context(), orgID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "subscription_not_found")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// handleCancelSubscription cancels the org's subscription at period end.
func (s *Server) handleCancelSubscription(w http.ResponseWriter, r *http.Request) {
	orgID, err := billingOrgFromRequest(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if s.BillingService == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "billing_unavailable")
		return
	}
	state, err := s.BillingService.CancelSubscription(r.Context(), orgID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "cancel_subscription_failed")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// handleGetInvoices returns the org's most recent Stripe invoices.
func (s *Server) handleGetInvoices(w http.ResponseWriter, r *http.Request) {
	orgID, err := billingOrgFromRequest(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if s.BillingService == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "billing_unavailable")
		return
	}
	invoices, err := s.BillingService.GetInvoices(r.Context(), orgID, 20)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list_invoices_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"invoices": invoices,
	})
}

// handleBillingWebhook verifies the Stripe signature header and
// acknowledges the event. Stripe requires a 2xx response within 30s.
func (s *Server) handleBillingWebhook(w http.ResponseWriter, r *http.Request) {
	if s.StripeClient == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "billing_unavailable")
		return
	}
	signature := r.Header.Get("Stripe-Signature")
	if signature == "" {
		writeJSONError(w, http.StatusBadRequest, "missing_signature")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read_body_failed")
		return
	}
	evt, err := s.StripeClient.VerifyWebhook(body, signature)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "signature_verification_failed")
		return
	}
	writeJSON(w, http.StatusOK, billingWebhookResponse{
		Received: true,
		EventID:  evt.ID,
	})
}

// handleGetUsage returns the current month's usage summary for the org.
func (s *Server) handleGetUsage(w http.ResponseWriter, r *http.Request) {
	orgID, err := billingOrgFromRequest(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if s.MeteringService == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "billing_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, s.MeteringService.GetUsage(orgID))
}
