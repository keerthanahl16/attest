// Package tools implements built-in mock handlers so demos need no external APIs.
package tools

import (
	"encoding/json"
	"fmt"
	"time"
)

// Execute runs a named mock handler against the step input JSON.
func Execute(handler string, input json.RawMessage) (json.RawMessage, error) {
	switch handler {
	case "issue_refund":
		return issueRefund(input)
	case "send_email":
		return sendEmail(input)
	case "echo":
		fallthrough
	default:
		out, _ := json.Marshal(map[string]any{
			"echo":       json.RawMessage(input),
			"handled_at": time.Now().UTC(),
		})
		return out, nil
	}
}

func issueRefund(input json.RawMessage) (json.RawMessage, error) {
	var body struct {
		Amount   float64 `json:"amount"`
		Currency string  `json:"currency"`
		OrderID  string  `json:"order_id"`
		Reason   string  `json:"reason"`
	}
	if err := json.Unmarshal(input, &body); err != nil {
		return nil, fmt.Errorf("invalid refund input: %w", err)
	}
	if body.Amount <= 0 {
		return nil, fmt.Errorf("amount must be positive")
	}
	if body.Currency == "" {
		body.Currency = "INR"
	}
	if body.OrderID == "" {
		return nil, fmt.Errorf("order_id is required")
	}
	out, err := json.Marshal(map[string]any{
		"refund_id":  fmt.Sprintf("rfnd_%d", time.Now().UnixNano()),
		"amount":     body.Amount,
		"currency":   body.Currency,
		"order_id":   body.OrderID,
		"reason":     body.Reason,
		"status":     "posted",
		"posted_at":  time.Now().UTC(),
		"mock":       true,
	})
	return out, err
}

func sendEmail(input json.RawMessage) (json.RawMessage, error) {
	var body struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal(input, &body); err != nil {
		return nil, fmt.Errorf("invalid email input: %w", err)
	}
	if body.To == "" || body.Subject == "" {
		return nil, fmt.Errorf("to and subject are required")
	}
	out, err := json.Marshal(map[string]any{
		"message_id": fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		"to":         body.To,
		"subject":    body.Subject,
		"status":     "accepted",
		"mock":       true,
		"sent_at":    time.Now().UTC(),
	})
	return out, err
}
