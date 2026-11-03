# Setu – Leads, Audits & CRM for agencies

Setu ("bridge") finds businesses on **Google Maps**, audits their **website** and **Google Business Profile**, writes a premium **AI audit report** as a beautifully designed PDF, sends it on **Email and WhatsApp** (PDF attached), and then runs your sales follow-up as a **CRM** with lead capture, **automations**, **AI voice calls** and **appointment booking**.

Built for **shared hosting** (cPanel / Hostinger / any PHP host): plain PHP 8 + MySQL, no framework, no Node, no Composer needed on the server. PDF (dompdf) and email (PHPMailer) libraries are bundled in `app/vendor`.

## Features
**Prospecting**
- **Map Scraper** – keyword + location, top 10/20/30/40/50/100 or any number (Google Places API up to 60, SerpAPI 100+). Missing phone numbers and emails are found on each website. Businesses already in your data are never added twice: repeat a search and only new businesses are added.
- **Google Places billing** – Google charges per *request*, not per business or per field. 1 Text Search request = 1 page of up to 20 businesses (20 dentists = 1 request, 60 = 3), billed at the highest SKU of the fields asked for: phone/website/rating/hours = *Text Search Enterprise* ($35 / 1,000 after 1,000 free a month). Asking only for place IDs is free and unlimited – Setu uses that first to skip businesses you already have. When just one new business is left on a page Setu fetches it with 1 *Place Details* request ($20 / 1,000) instead. Business descriptions (editorial summary) move requests to the *Enterprise + Atmosphere* SKU and are off by default.
- **Scraped Data** – all scraped businesses, kept separate from your CRM. Bulk *Initiate Process*, *Send outreach*, *Move to Leads*, *Abort*.
- **Deep audit** – ~45 website checks in 8 areas (technical, performance, on-page SEO, content, conversion, local SEO, trust, social: SSL expiry, redirects, broken links, inner-page crawl, image weight, render-blocking files, schema, NAP consistency, domain age, optional PageSpeed/Core Web Vitals) plus a Google Maps audit benchmarked against competitors.
- **Premium AI reports** – 15+ page branded PDF: cover, health-score gauges, score-by-area bars, top priorities, competitor review/rating charts, AI-written deep-dive (optionally web-researched by Claude), revenue opportunity with ROI chart, 90-day plan, quick wins, about-us and CTA. Built-in illustrations in your brand colours, or **upload your own doodles/cartoons per section and per prompt**.
- **Fast bulk reports** – with any AI provider a parallel engine writes several reports at once (default 5), streaming each with live progress and saving each the moment it is done; rate limits are retried automatically. Optional 'Cheapest' mode sends a Claude run as one Message Batch (50% off). Market research is shared per business type & city.
- **Live progress & abort** – every audit/report shows its % and current step; stop one item or everything from the **Activity** panel. Stuck steps time out with a clear error.
- **Outreach** – email with the PDF attached; WhatsApp Cloud API uploads the PDF as a real attachment (no link). Manual mode shares the PDF straight into WhatsApp on mobile.

**CRM**
- **Leads** – won or manually moved scraped businesses plus every captured enquiry. List and drag-and-drop **board**, statuses, owners, follow-ups, tags, deal value, **custom fields**, CSV import/export, filters by source (Map scraper, Website, Meta, Google Ads, Webhook, Manual, Import, Booking).
- **Emails** – one inbox for all email with leads: conversations per lead, replies pulled from your mailbox by IMAP every 2 minutes, reply or start new emails (report PDF attachable) without opening your email provider.
- **Lead capture** (Settings → Lead capture) – website forms (ready-made form or attach to existing forms), **Meta / Facebook Lead Ads** webhook, **Google Ads lead form** webhook, generic webhook for Zapier / Make / n8n / Pabbly. Duplicates are merged.
- **Automations** – trigger (new lead, status change, tag, reply, report ready, appointment, call finished, no reply after N days, follow-up due) → conditions → steps: WhatsApp, email, **AI voice call (Sarvam AI, Bland, Vapi, Retell or any API)**, booking link, wait, status, tag, assign, follow-up, team notification, webhook, run audit. Ready-made recipes included. Every step is saved in the lead's journey.
- **Appointments** – public booking page (general and personal links), calendar, confirmations, 1-hour reminders.
- **Journey** – a complete timeline per lead: found, audited, sent, opened, viewed, replied, calls, bookings, automation steps, notes.

**Platform**
- **Modules** – CRM is always on; **Prospecting** can be switched off by a super admin (its menu, pages and settings are hidden, data is kept). Settings are grouped by module: General, Messaging, Prospecting, CRM, System.
- **Roles** – *Super admin* (everything incl. modules and other super admins), *Admin / Manager* (all leads, prospecting, automations, templates, settings, team), *Sales person* (own + unassigned leads, add leads, notes, status, appointments). Manage people in **Team & Roles**.
- **API Usage & Costs** – counts every AI (tokens per model), Google Places, SerpAPI, PageSpeed, WhatsApp, AI voice (Sarvam, Bland, Vapi, Retell), email and Google Calendar call. Shows free limits, month-end projection, estimated cost (USD and your currency) and warns before you go over a free tier. Prices are editable.
- **Google Calendar (optional)** – bookings stay in Setu; when connected, busy times in Google block slots, each booking becomes a calendar event with a Meet link and invite, cancellations remove it.
- **Email** – PHP mail by default (works on shared hosting); SMTP optional for better delivery, with ready-made settings for Brevo, Mailjet, SendGrid, Mailgun, Amazon SES, ZeptoMail, Postmark, Resend, SMTP2GO, Elastic Email, Gmail, Zoho, Microsoft 365 and Hostinger (paste the SMTP key / API key as the password).
- AI Connection (Claude, ChatGPT, OpenRouter, Gemini, Groq, DeepSeek, custom), automatic background scheduler (no cron needed), one-click GitHub updates (Releases only or Latest commit), configurable currency (default INR).

## Audit report delivery
Settings → Prospecting → **Report delivery**: send businesses an **online report link** (a branded web page with Download PDF, WhatsApp and Book-a-call buttons), a **PDF attachment**, or **both**. PDFs are made by the free built-in engine – no external service needed. Before writing, the AI runs a research step (live web search with Claude) and the report lists its sources.

## Lead capture – which method?
Use **one** method per form:
- **Elementor / WordPress form plugins (recommended):** Form widget → *Actions After Submit* → **Webhook** → paste the source's Endpoint URL. Simple and "Advanced data" payloads both work.
- **Any other form without a webhook option:** paste the source's script before `</body>`.
- **No form yet:** paste the ready-made HTML form.
Don't combine the webhook and the script for the same form – each enquiry would arrive twice (duplicates are merged by phone/email, but the activity doubles).
Field mapping: common names (name, phone/mobile/whatsapp, email, company, city, service, budget, message) and your custom field keys are matched automatically; anything else goes to the enquiry notes. After the first submission every field appears under **Settings → CRM → Lead capture → Field mapping**, where you can send it to any field or ignore it.

## Security
- Website audits and contact scans only fetch public internet addresses (private, loopback and cloud-metadata IPs are blocked on every redirect).
- Login is throttled per IP address (10 failures → 15 minutes); public lead forms and the booking page are rate-limited.
- CSRF tokens on every form (logout included), hardened session cookies, no open redirects, CSV exports protected against formula injection, API keys encrypted in the database.
- On Nginx hosts (no .htaccess) block `/app`, `/storage` and `config.php` in the server config; log file names are randomised per install.

## Install (shared hosting)
1. Upload the files to `public_html` or a sub-domain folder (Download ZIP from GitHub → File Manager → Extract), or use cPanel → Git Version Control.
2. Create a MySQL database and user (cPanel → MySQL Database Wizard, ALL PRIVILEGES).
3. Open `https://your-domain/install.php`, enter the DB details and create the admin login.
4. Background jobs run automatically – no cron setup needed. Setu adds a server cron itself when the host allows it, otherwise its built-in scheduler keeps running and restarts on any visit. (Optional extra reliability: a cron job every minute with `php /path/to/cron.php`.)
5. Connect AI (AI Connection tab), the Google Places / SerpAPI key, email (PHP mail works out of the box; SMTP/IMAP optional) and WhatsApp (Settings). The installer's admin account is the **super admin**.

Requirements: PHP 8.0+ with pdo_mysql, curl, mbstring, dom, gd, openssl, and zip (for updates). Also MySQL 5.7+ or MariaDB 10.3+, and HTTPS.

Each step is a separate short job, so strict `max_execution_time` limits are fine.

## Releasing an update
Repository: https://github.com/narendersinghh/Setu-crm
1. Commit your changes to the update branch (default `main`, configurable in Updates).
2. Bump `VERSION` and `version.json` (`{"version":"1.0.1","date":"…","notes":["…"]}`).
3. Publish a GitHub release with the same tag (e.g. `v1.0.1`). Sites on **Releases only** (default) see it then; sites on **Latest commit** see every push.
4. For database changes add `app/migrations/002_xxx.php` returning an array of SQL statements. It runs automatically after an update.
Installed sites show a **New** badge on Updates and can update with one click. For a private repository, add a read-only GitHub token in the Updates tab.

## Structure
```
index.php        app entry (routes ?r=page)      install.php  installer
cron.php         background queue + replies       report.php   public PDF link (tracked)
track.php        email open pixel                 webhook.php  WhatsApp Cloud API webhook
hook.php         lead capture (forms/Meta/Google) callhook.php AI voice call results
book.php         public booking page
app/lib          DB, Settings, Maps, Scraper, Auditor, AI, Report, Charts, Illus, Mailer, WhatsApp, Queue, Outreach,
                 CRM, Capture, Automation, Voice, Booking, GCal, Usage, Cron, Updater, Migrator, Seed
app/pages        dashboard, scraper, campaign, prospects, leads, lead, lead_new, appointments, automations, automation,
                 templates, reports, settings, ai, usage, team, user, account, updates, api, login
gcal.php         Google Calendar connect (OAuth)
app/views        layout + shared partials          app/migrations  database schema
assets/          css, js, images, uploaded logo    storage/  reports, uploads, fonts, logs, backups (private)
```

## Notes
- Google Places API returns a maximum of 60 results per query. Use SerpAPI for more.
- WhatsApp Cloud API needs an approved template for the first (business-initiated) message. Manual mode needs no approval.
- Send responsibly: keep the gaps and daily limits sensible, and honour opt-out requests. Follow anti-spam and data-protection rules (e.g. India's DPDP Act and IT rules, GDPR for EU contacts).
