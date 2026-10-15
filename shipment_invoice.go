package invoicemailer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"
)

type ShipmentEvent struct {
	Kind       string    `json:"kind"`
	OccurredAt time.Time `json:"occurred_at"`
}

type ProofOfDelivery struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type ShipmentException struct {
	Code       string `json:"code"`
	Resolution string `json:"resolution"`
}

type InvoiceRequest struct {
	ShipmentID    string              `json:"shipment_id"`
	CustomerEmail string              `json:"customer_email"`
	Amount        string              `json:"amount"`
	Currency      string              `json:"currency"`
	Events        []ShipmentEvent     `json:"events"`
	Proofs        []ProofOfDelivery   `json:"proofs"`
	Exceptions    []ShipmentException `json:"exceptions"`
}

type InvoiceDecision struct {
	Ready  bool
	Reason string
}

func DecideInvoice(s InvoiceRequest) InvoiceDecision {
	delivered := false
	for _, event := range s.Events {
		if event.Kind == "delivered" {
			delivered = true
		}
	}
	if !delivered {
		return InvoiceDecision{Reason: "shipment is not delivered"}
	}
	if len(s.Proofs) == 0 {
		return InvoiceDecision{Reason: "proof of delivery is missing"}
	}
	for _, exception := range s.Exceptions {
		if strings.TrimSpace(exception.Resolution) == "" {
			return InvoiceDecision{Reason: "shipment has an unresolved exception"}
		}
	}
	return InvoiceDecision{Ready: true, Reason: "ready for invoicing"}
}

type InvoiceGateway interface {
	GeneratePDF(context.Context, PDFGenerateRequest, string) (GeneratedPDF, error)
	SendEmail(context.Context, EmailSendRequest, string) (SentEmail, error)
}

type InvoiceService struct {
	gateway InvoiceGateway
}

func NewInvoiceService(gateway InvoiceGateway) *InvoiceService {
	return &InvoiceService{gateway: gateway}
}

func (s *InvoiceService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var input InvoiceRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	decision := DecideInvoice(input)
	if !decision.Ready {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": decision.Reason})
		return
	}

	html, err := renderInvoice(input)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not render invoice"})
		return
	}
	idempotencyKey := "shipment-invoice-" + input.ShipmentID
	pdf, err := s.gateway.GeneratePDF(r.Context(), PDFGenerateRequest{
		HTML: html, PageSize: "A4", Orientation: "portrait", Store: true,
	}, idempotencyKey+"-pdf")
	if err != nil {
		writeGatewayError(w, err)
		return
	}

	mailHTML := fmt.Sprintf("<p>Your shipment %s is complete.</p><p><a href=\"%s\">Download invoice PDF</a></p>", template.HTMLEscapeString(input.ShipmentID), template.HTMLEscapeString(pdf.URL))
	message, err := s.gateway.SendEmail(r.Context(), EmailSendRequest{
		To: input.CustomerEmail, Subject: "Invoice for shipment " + input.ShipmentID, HTML: mailHTML,
	}, idempotencyKey+"-email")
	if err != nil {
		writeGatewayError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"shipment_id": input.ShipmentID,
		"pdf_url":     pdf.URL,
		"message_id":  message.MessageID,
	})
}

func renderInvoice(input InvoiceRequest) (string, error) {
	const document = `<!doctype html><html><body><h1>Logistics invoice</h1><dl><dt>Shipment</dt><dd>{{.ShipmentID}}</dd><dt>Amount</dt><dd>{{.Amount}} {{.Currency}}</dd><dt>Proof of delivery</dt>{{range .Proofs}}<dd><a href="{{.URL}}">{{.Name}}</a></dd>{{end}}</dl></body></html>`
	t, err := template.New("invoice").Parse(document)
	if err != nil {
		return "", err
	}
	var output strings.Builder
	if err := t.Execute(&output, input); err != nil {
		return "", err
	}
	return output.String(), nil
}

func writeGatewayError(w http.ResponseWriter, err error) {
	var apiErr *InfraiError
	if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 {
		writeJSON(w, apiErr.Status, map[string]string{"error": apiErr.Message, "code": apiErr.Code})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "invoice delivery failed"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
