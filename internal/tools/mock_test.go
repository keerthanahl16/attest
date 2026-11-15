package tools

import (
	"encoding/json"
	"testing"
)

func TestIssueRefundRequiresOrderID(t *testing.T) {
	_, err := Execute("issue_refund", json.RawMessage(`{"amount":100}`))
	if err == nil {
		t.Fatal("expected error for missing order_id")
	}
}

func TestIssueRefundHappyPath(t *testing.T) {
	out, err := Execute("issue_refund", json.RawMessage(`{"amount":5000,"currency":"INR","order_id":"ord_1","reason":"demo"}`))
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "posted" {
		t.Fatalf("unexpected status: %v", body["status"])
	}
}

func TestSendEmail(t *testing.T) {
	out, err := Execute("send_email", json.RawMessage(`{"to":"a@b.com","subject":"hi","body":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "accepted" {
		t.Fatalf("unexpected status: %v", body["status"])
	}
}
