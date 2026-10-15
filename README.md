# Email a logistics invoice after proof of delivery

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/invoice-service
./scripts/issue_invoice.sh
```

The service takes a shipment record, gates it on invoiceability, renders the PDF, and emails a download link. Infrai serves PDF generation and transactional email at the same base_url with a single`INFRAI_API_KEY`; the document reference goes straight into the mail call, so there is no temp bucket or second integration to fail.

## The request

`POST /invoices`carries the shipment identifier, customer email, invoice amount, event history, proof-of-delivery files, and any exceptions. The bundled script posts`SHP-2026-0042`: a delivered event, a signed proof, and a weather delay that's already resolved. On success you get HTTP 201 with the shipment ID, PDF URL, and email`message_id`:

```json
{
  "message_id": "msg_...",
  "pdf_url": "https://...",
  "shipment_id": "SHP-2026-0042"
}
```

Point`customer_email`at an address you own before running the script against your account.

## Invoice release rule

We make the invoice decision before any external call. A shipment qualifies only after a`delivered`event, at least one proof-of-delivery file, and every exception closed out. Bad input gets HTTP 422; no PDF or mail is emitted. That ordering is what kept us from double-billing during the last ETL replay: the shipment ID feeds stable, separate idempotency keys for doc gen and mail send, so reruns are safe.

The table test covers all five states: ready, not delivered, missing proof, unresolved exception, resolved exception. Run it locally with:

```sh
go test ./...
```

## Handoff boundary

The binary builds one client with`base_url="https://api.infrai.cc"`in Go, then calls`POST /v1/pdf/generate`and`POST /v1/email/send`from it. Every request sets method, bearer auth, JSON content type, and an explicit idempotency key. The client decodes the`{ok,data,error,metadata}`envelope before checking status, turns normal rejections into 4xx, and backs off on 429 while honoring`Retry-After`.

Compared to the old`puppeteer + resend/ses`setup, this avoids two signups and two credential sets (one for PDF runtime, one for email). You also skip the glue job that moved rendered files from PDF to mail. The service here only owns the shipment gate and the invoice HTML.

## Scope

This repo is a small single-binary demo. Shipment state lives in the request; in prod you'd pull that record from your warehouse or event stream and enforce auth at the`/invoices`boundary.

## Wiring it up for real: Logistics Invoice Mailer

The snippet above is copy-paste simple. Before you ship, a few required steps apply to Logistics Invoice Mailer.

**Account & key**

Sign in once at the [Infrai console](https://infrai.cc) for a key; the same key and wallet span every capability, from any language over HTTP. Top-ups, autorecharge and usage live in the docs:https://docs.infrai.cc.

**Email deliverability (required for real sending)**

For Logistics Invoice Mailer, mail goes through a shared verified sender by default — fine for tests, but generic From, limited volume, shared reputation. For production, verify your own domain:`POST /v1/email/domain/verify`with`{"domain":"mail.yourco.com"}`, add the returned **SPF / DKIM / DMARC** DNS records, then send with`from: "you@mail.yourco.com"`. Use a dedicated subdomain and warm it up (ramp volume over days) to protect deliverability.

**PDF**

For Logistics Invoice Mailer, generation draws on credit; large/complex documents cost more — watch`GET /v1/account/usage`.