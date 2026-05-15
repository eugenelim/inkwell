# Third-party fixture attribution

The `real_*.eml` fixtures in this directory wrap HTML bodies taken from the
[`Mailteorite/mjml-email-templates`](https://github.com/Mailteorite/mjml-email-templates)
collection (commit fetched 2026-05-07). Those templates are MIT-licensed; the
license text is reproduced below per its terms. The RFC 5322 envelopes (`From`,
`To`, `Subject`, `Date`, `Message-ID`, etc.) wrapped around each HTML body were
written for this repository and are covered by inkwell's own license.

**Modifications.** Brand-placeholder domains the upstream templates ship with
(`postable.com`, `yourskincare.com`, `yourreviewlink.com`) were replaced with
`example.invalid` to comply with `docs/CONVENTIONS.md` §7's privacy rule that test fixtures
use only the synthetic IANA-reserved domain. Public CDN references in `href`
attributes (Google Fonts, image hosts, social-media profile URLs) are
technical infrastructure pointers, not PII, and were left intact.

The hand-crafted `min_*.eml` fixtures in this directory are original to this
repository.

## MIT License — Mailteorite/mjml-email-templates

```
MIT License

Copyright (c) 2025 Mailteorite

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## File provenance

| Fixture | Upstream source |
| --- | --- |
| `real_newsletter_data_analysis.eml` | `html/newsletter/01-weekly-data-analysis.html` |
| `real_review_request.eml` | `html/receipt-invoice/01-review-request-post-delivery.html` |
| `real_card_shipped.eml` | `html/shipping-update/01-card-shipped.html` |
