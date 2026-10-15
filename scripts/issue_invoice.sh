#!/bin/sh
set -eu

curl --fail-with-body --request POST http://localhost:8080/invoices \
  --header 'Content-Type: application/json' \
  --data '{
    "shipment_id": "SHP-2026-0042",
    "customer_email": "customer@example.com",
    "amount": "1840.00",
    "currency": "USD",
    "events": [{"kind":"delivered","occurred_at":"2026-09-25T10:00:00Z"}],
    "proofs": [{"name":"signed delivery note","url":"https://files.example.com/pod/SHP-2026-0042"}],
    "exceptions": [{"code":"WEATHER_DELAY","resolution":"customer accepted revised delivery date"}]
  }'
