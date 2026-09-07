package accounting

import (
	"fmt"
	"strings"

	"github.com/matjeroapps/core/packages/events"
)

type JournalPostingRule struct {
	ShouldPost        bool
	ReferenceType     string
	ReferenceID       string
	Description       string
	Currency          string
	AmountMinor       int64
	DebitAccountRole  AccountRole
	CreditAccountRole AccountRole
	Reason            string
}

type RuleEngine interface {
	ResolveRule(eventType string, payloadMap map[string]any) (JournalPostingRule, error)
}

type ruleEngine struct{}

func NewRuleEngine() RuleEngine {
	return &ruleEngine{}
}

func (e *ruleEngine) ResolveRule(eventType string, payloadMap map[string]any) (JournalPostingRule, error) {
	switch eventType {
	case events.EventTypePaymentCaptured:
		return e.resolveCapturedPayment(payloadMap)
	case events.EventTypePaymentFailed:
		return e.resolveFailedPayment(payloadMap)
	default:
		return JournalPostingRule{}, fmt.Errorf("%w: %s", ErrUnsupportedEventType, eventType)
	}
}

func (e *ruleEngine) resolveCapturedPayment(payloadMap map[string]any) (JournalPostingRule, error) {
	paymentID, _ := payloadMap["payment_id"].(string)
	orderID, _ := payloadMap["order_id"].(string)
	currency, _ := payloadMap["currency"].(string)

	var amountMinor int64
	switch v := payloadMap["amount_minor"].(type) {
	case int64:
		amountMinor = v
	case float64:
		amountMinor = int64(v)
	}

	paymentID = strings.TrimSpace(paymentID)
	orderID = strings.TrimSpace(orderID)
	currency = strings.ToUpper(strings.TrimSpace(currency))

	if paymentID == "" || orderID == "" || currency == "" || amountMinor <= 0 {
		return JournalPostingRule{}, fmt.Errorf("%w: invalid captured payment fields", ErrInvalidEventPayload)
	}

	return JournalPostingRule{
		ShouldPost:        true,
		ReferenceType:     "PAYMENT_CAPTURED",
		ReferenceID:       paymentID,
		Description:       fmt.Sprintf("Payment captured for order %s", orderID),
		Currency:          currency,
		AmountMinor:       amountMinor,
		DebitAccountRole:  RolePaymentClearing,
		CreditAccountRole: RoleCustomerFunds,
	}, nil
}

func (e *ruleEngine) resolveFailedPayment(payloadMap map[string]any) (JournalPostingRule, error) {
	paymentID, _ := payloadMap["payment_id"].(string)
	if strings.TrimSpace(paymentID) == "" {
		return JournalPostingRule{}, fmt.Errorf("%w: missing payment_id in failed payment payload", ErrInvalidEventPayload)
	}

	return JournalPostingRule{
		ShouldPost: false,
		Reason:     "No financial posting required for payment failed event",
	}, nil
}
