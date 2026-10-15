package invoicemailer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDecideInvoice(t *testing.T) {
	delivered := ShipmentEvent{Kind: "delivered", OccurredAt: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)}
	proof := ProofOfDelivery{Name: "signed-pod.pdf", URL: "https://files.example.test/pod/ship-42"}
	tests := []struct {
		name string
		in   InvoiceRequest
		want InvoiceDecision
	}{
		{name: "ready after delivery and proof", in: InvoiceRequest{Events: []ShipmentEvent{delivered}, Proofs: []ProofOfDelivery{proof}}, want: InvoiceDecision{Ready: true, Reason: "ready for invoicing"}},
		{name: "wait before delivery", in: InvoiceRequest{Proofs: []ProofOfDelivery{proof}}, want: InvoiceDecision{Reason: "shipment is not delivered"}},
		{name: "wait for proof", in: InvoiceRequest{Events: []ShipmentEvent{delivered}}, want: InvoiceDecision{Reason: "proof of delivery is missing"}},
		{name: "hold unresolved exception", in: InvoiceRequest{Events: []ShipmentEvent{delivered}, Proofs: []ProofOfDelivery{proof}, Exceptions: []ShipmentException{{Code: "DAMAGED"}}}, want: InvoiceDecision{Reason: "shipment has an unresolved exception"}},
		{name: "continue after resolution", in: InvoiceRequest{Events: []ShipmentEvent{delivered}, Proofs: []ProofOfDelivery{proof}, Exceptions: []ShipmentException{{Code: "DELAY", Resolution: "accepted"}}}, want: InvoiceDecision{Ready: true, Reason: "ready for invoicing"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecideInvoice(tt.in); got != tt.want {
				t.Fatalf("DecideInvoice() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestInvoiceServiceHandsPDFToEmail(t *testing.T) {
	gateway := &recordingGateway{}
	service := NewInvoiceService(gateway)
	body := `{"shipment_id":"SHP-42","customer_email":"buyer@example.com","amount":"75.00","currency":"USD","events":[{"kind":"delivered","occurred_at":"2026-09-25T10:00:00Z"}],"proofs":[{"name":"signed note","url":"https://files.example.test/pod/42"}],"exceptions":[]}`
	req := httptest.NewRequest(http.MethodPost, "/invoices", strings.NewReader(body))
	res := httptest.NewRecorder()

	service.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusCreated, res.Body.String())
	}
	if !gateway.pdfInput.Store || gateway.pdfInput.PageSize != "A4" {
		t.Fatalf("PDF request = %#v", gateway.pdfInput)
	}
	if !strings.Contains(gateway.mailInput.HTML, "https://documents.example.test/invoice-42.pdf") {
		t.Fatalf("email did not receive generated PDF URL: %q", gateway.mailInput.HTML)
	}
	if gateway.pdfKey != "shipment-invoice-SHP-42-pdf" || gateway.mailKey != "shipment-invoice-SHP-42-email" {
		t.Fatalf("idempotency keys = %q, %q", gateway.pdfKey, gateway.mailKey)
	}
}

func TestInfraiClientIncludesIdempotencyKeyInRequestBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		if !strings.Contains(string(body), `"idempotency_key":"request-key"`) {
			t.Fatalf("request body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":{"url":"https://documents.example.test/invoice.pdf"}}`))
	}))
	defer server.Close()

	client := NewInfraiClient(server.URL, "test-key", server.Client())
	if _, err := client.GeneratePDF(context.Background(), PDFGenerateRequest{HTML: "<p>invoice</p>"}, "request-key"); err != nil {
		t.Fatal(err)
	}
}

type recordingGateway struct {
	pdfInput  PDFGenerateRequest
	mailInput EmailSendRequest
	pdfKey    string
	mailKey   string
}

func (g *recordingGateway) GeneratePDF(_ context.Context, in PDFGenerateRequest, key string) (GeneratedPDF, error) {
	g.pdfInput, g.pdfKey = in, key
	return GeneratedPDF{URL: "https://documents.example.test/invoice-42.pdf"}, nil
}

func (g *recordingGateway) SendEmail(_ context.Context, in EmailSendRequest, key string) (SentEmail, error) {
	g.mailInput, g.mailKey = in, key
	return SentEmail{MessageID: "msg_42"}, nil
}
