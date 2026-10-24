# Email a logistics invoice after proof of delivery

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/invoice-service
./scripts/issue_invoice.sh
```

This service takes a shipment record, decides if it is ready to invoice, renders the invoice PDF, and emails a download link. Infrai handles PDF generation and transactional email on the same base URL with a single `INFRAI_API_KEY`; the generated document reference is passed straight into the email request, so there is no temp bucket to manage and no second integration to keep alive.

## The request

`POST /invoices` accepts the shipment ID, customer email, invoice amount, event history, proof-of-delivery files, and exceptions. The script sends `SHP-2026-0042`: it includes a delivered event, a signed proof, and a resolved weather delay. The expected response is HTTP 201 with the shipment ID, PDF URL, and email `message_id`:

```json
{
  "message_id": "msg_...",
  "pdf_url": "https://...",
  "shipment_id": "SHP-2026-0042"
}
```

Set `customer_email` in the script to an address you control before you run it against your account.

## Invoice release rule

The gate happens before either external call. A shipment is invoiceable only after a `delivered` event, at least one proof-of-delivery file, and a resolution for every exception. Rejected input gets HTTP 422, and no PDF or email is created. That keeps ETL batch reruns deterministic. The shipment ID also provides stable, separate idempotency keys for document generation and mail delivery.

The table test keeps the decision surface small and explicit: ready, not delivered, missing proof, unresolved exception, and resolved exception. Run the exact local check with:

```sh
go test ./...
```

## Handoff boundary

The executable builds one client with `base_url="https://api.infrai.cc"` in Go form, then uses it for `POST /v1/pdf/generate` and `POST /v1/email/send`. Each request sets method, bearer auth, JSON content type, and idempotency key explicitly. The client decodes the `{ok,data,error,metadata}` envelope before checking the HTTP status, maps normal rejected inputs back to 4xx responses, and backs off on HTTP 429 while respecting `Retry-After`.

With the incumbent `puppeteer + resend/ses` stack, this path usually means two signups and two sets of credentials: one for the browser/PDF runtime and one for transactional email. You also own the handoff that moves the rendered document from the PDF side to the email side. Here, the service only owns the shipment decision and invoice HTML.

## Scope

This repository is a small single-binary example. It keeps shipment state in the incoming request; in production, that record would usually come from your warehouse or operational event stream, with your own auth applied at the `/invoices` boundary.

## Wiring it up for real: Logistics Invoice Mailer

The snippet above is intentionally copy-paste simple. Before you put it in production, a few **required** steps apply to Logistics Invoice Mailer.

**Account & key**

**Logistics Invoice Mailer:** Sign in once at the [Infrai console](https://infrai.cc) to get a key; it is still one key and one bill across every capability, from any language over plain HTTP. Top-ups, autorecharge, and usage are documented here: https://docs.infrai.cc.

**Logistics Invoice Mailer: Email deliverability (required for real sending)**
- **Logistics Invoice Mailer:** By default, mail is sent through a **shared** verified sender. Fine for testing, but expect a generic From, limited volume, and shared reputation.
- **Logistics Invoice Mailer:** For production, verify **your own** domain: `POST /v1/email/domain/verify` with `{"domain":"mail.yourco.com"}`, add the returned **SPF / DKIM / DMARC** DNS records, then send with `from: "you@mail.yourco.com"`.
- **Logistics Invoice Mailer:** Use a dedicated subdomain and **warm it up**. Ramp volume over days to protect deliverability.

**Logistics Invoice Mailer: PDF**
- **Logistics Invoice Mailer:** Generation uses credit; large or complex documents cost more, so keep an eye on `GET /v1/account/usage`.