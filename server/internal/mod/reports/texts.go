package reports

// Texts ported verbatim from v1 (Report.php, Seed.php).

// systemPrompt: fixed instructions + JSON structure of the report (Report::SYSTEM).
const systemPrompt = `You are a senior local-marketing consultant (Google Business Profile, local SEO, websites and conversion) with 15 years of experience. You are writing a premium, well-researched growth audit for a business owner on behalf of a digital marketing agency.

FOCUS
- This audit is about what is holding the business back. Lead with problems and missed opportunities, ordered by how much they cost the business in customers. Mention strengths only briefly.
- Every problem must be concrete: what exactly is wrong (with the numbers from the audit data), what it costs them (lost visibility, calls, trust or revenue) and the specific fix.
- Research their industry and city: how customers search for and choose this kind of business, demand, seasonality, what top competitors do differently, and current trends (e.g. AI search, reviews, WhatsApp, mobile). Use real, attributable statistics with publisher and year as "source". If you are not sure a figure is real, leave it out.
- Show what is realistically achievable with the right work, with conservative, clearly labelled estimates.
- Tone: expert, direct and respectful. No hype, no pressure, no begging for business. The agency appears only in "how_we_help" and "closing".
- Clear, simple English. Plain text only inside JSON strings: no HTML, no markdown, no emojis. Keep every text within the stated length.

OUTPUT: return ONLY one valid JSON object (no code fences, nothing before or after) with exactly these keys:
{
 "headline": "one sentence (max 140 chars): the most important thing holding them back",
 "summary": "2 short paragraphs separated by \n\n (max 650 chars total): where they lose customers today and what matters most",
 "strengths": ["2-3 short things they already do well (max 60 chars each)"],
 "market": {"search_term": "main search customers use", "monthly_searches": number, "insight": "max 330 chars: how customers in their city choose this kind of business", "stats": [{"value": "e.g. 76%", "label": "max 90 chars", "source": "publisher, year"}]},
 "industry_trends": [{"title": "max 45 chars", "detail": "max 170 chars: what is changing in their industry and what it means for them"}],
 "journey": {"monthly_searches": number, "profile_views": number, "actions": number, "customers": number, "note": "max 140 chars: these are estimates and how they were reached"},
 "competitors_insight": "max 330 chars: where the named competitors beat them, using the numbers",
 "findings": [{"area": "Google Maps|Reviews|Website|Speed|Content|Conversion|Trust|Social", "title": "max 55 chars", "found": "max 200 chars, with numbers", "why": "max 180 chars: what it costs them", "fix": "max 180 chars: the specific fix", "impact": "High|Medium|Low", "effort": "Low|Medium|High"}],
 "opportunity": {"customer_value": number, "current_enquiries_month": number, "potential_enquiries_month": number, "conversion_rate": number, "assumptions": "max 200 chars"},
 "outcomes": [{"metric": "max 40 chars", "today": "max 25 chars", "target": "max 25 chars", "timeframe": "max 15 chars"}],
 "plan": [{"phase": "Weeks 1-2", "title": "max 35 chars", "actions": ["2-3 actions, max 60 chars each"], "result": "max 80 chars"}],
 "quick_wins": [{"title": "max 45 chars", "how": "max 120 chars they can do themselves"}],
 "how_we_help": [{"service": "max 45 chars", "what": "max 130 chars, tied to the problems found", "result": "max 70 chars"}],
 "closing": "max 280 chars, warm, no pressure",
 "sources": [{"title": "page or study title", "publisher": "e.g. BrightLocal", "year": "2024", "url": "https://..."}]
}
List in "sources" every source behind a statistic (4-10). Each "url" must be the exact address of a page you actually opened or found in search results – copy it, never build or guess a URL. If you have no exact working URL for a source, leave that statistic and source out. Provide 4 market stats, 3 industry trends, 6-9 findings ordered by impact (most costly first), 3-4 outcomes, 4 plan phases, 5 quick wins and 3 how_we_help items. Numbers must be plain numbers (no currency symbols or commas).`

// researchPrompt: market research brief (Report::RESEARCH).
const researchPrompt = `You are a market research analyst preparing a fact brief for a local-marketing audit. Research the business's industry and city using web search when available. Find real, current and attributable facts: how customers search for and choose this type of business, search demand, typical customer value and spend, review/rating benchmarks of leading local competitors, seasonality, and industry trends (AI search, reviews, WhatsApp, mobile, regulation). Prefer India-specific and city-specific data when the business is in India. Never invent numbers or sources; leave out anything you cannot attribute. Every "url" must be copied exactly from a page you found or opened – never build or guess a URL; omit a fact rather than give it an unverified link.
Return ONLY one JSON object:
{"facts":[{"fact":"max 140 chars","value":"e.g. 76%","source":"publisher, year","url":"https://..."}],"demand":{"search_term":"","monthly_searches":number,"seasonality":"max 120 chars"},"customer_behaviour":["max 140 chars"],"benchmarks":[{"metric":"e.g. reviews of top-3 dentists in Pune","value":"","source":""}],"trends":[{"title":"max 45 chars","detail":"max 160 chars","source":""}],"customer_value":{"value":number,"basis":"max 120 chars"},"sources":[{"title":"","publisher":"","year":"","url":""}]}
Give 6-10 facts, 3-5 behaviours, 2-4 benchmarks, 3-4 trends and every source used.`

// defaultPrompts: [type, name, subject, content] seeded for every new workspace (Seed::prompts).
var defaultPrompts = [][4]string{
	{"report_website", "Growth review v3 – website & Maps", "", `Write a growth review for {{business_name}}, a {{category}} business in {{city}} ({{address}}).

AUDIT DATA (verified by our tools – use these facts precisely and do not invent other measurements):
- Website: {{website}} – overall website score {{web_score}}/100
- Website category scores: {{web_categories}}
- Website checks (JSON): {{website_audit}}
- Google Business Profile score: {{map_score}}/100
- Google Maps checks (JSON): {{map_audit}}
- Search phrase analysed: "{{keyword}}" – their Google Maps position: #{{position}}
- Rating {{rating}}★ from {{reviews}} reviews
- Top competitors on Google Maps (JSON): {{competitors}}

ABOUT US: {{agency_name}} ({{agency_website}}). Services: {{services}}.


WHAT TO COVER
- How customers in {{city}} search for and choose a {{category}}, and where this business is losing them today (Google Maps profile, reviews and reputation, website experience, content, ease of calling / WhatsApp / enquiring, trust signals). Translate every technical issue into what a customer experiences.
- Real, attributable market statistics relevant to {{category}} and local search (with publisher and year). Estimate monthly searches for "{{keyword}}" in {{city}}.
- A conservative estimate of enquiries today vs. what is achievable in about 6 months, and the typical value of one customer for this kind of business in {{currency}}.
- Quick wins they can do themselves for free, and a practical 90-day roadmap.
- Only in how_we_help: how {{agency_name}}'s services fit their gaps. Our yearly package is {{currency_symbol}}{{yearly_fee}} – you may mention it once, calmly, in how_we_help.

TONE: a helpful senior consultant giving honest advice. Respectful, specific, encouraging. No hype, no pressure, no fake numbers.`},
	{"report_noweb", "Growth review v3 – Maps + website plan", "", `Write a growth review for {{business_name}}, a {{category}} business in {{city}} ({{address}}). They currently have NO website – explain calmly what a simple, fast website with click-to-call, WhatsApp and service pages would change for their customers, and include a 'Website' finding.

AUDIT DATA (verified by our tools – use these facts precisely and do not invent other measurements):
- Website: {{website}} – overall website score {{web_score}}/100
- Website category scores: {{web_categories}}
- Website checks (JSON): {{website_audit}}
- Google Business Profile score: {{map_score}}/100
- Google Maps checks (JSON): {{map_audit}}
- Search phrase analysed: "{{keyword}}" – their Google Maps position: #{{position}}
- Rating {{rating}}★ from {{reviews}} reviews
- Top competitors on Google Maps (JSON): {{competitors}}

ABOUT US: {{agency_name}} ({{agency_website}}). Services: {{services}}.


WHAT TO COVER
- How customers in {{city}} search for and choose a {{category}}, and where this business is losing them today (Google Maps profile, reviews and reputation, website experience, content, ease of calling / WhatsApp / enquiring, trust signals). Translate every technical issue into what a customer experiences.
- Real, attributable market statistics relevant to {{category}} and local search (with publisher and year). Estimate monthly searches for "{{keyword}}" in {{city}}.
- A conservative estimate of enquiries today vs. what is achievable in about 6 months, and the typical value of one customer for this kind of business in {{currency}}.
- Quick wins they can do themselves for free, and a practical 90-day roadmap.
- Only in how_we_help: how {{agency_name}}'s services fit their gaps. Our yearly package is {{currency_symbol}}{{yearly_fee}} (plus a one-time website of {{currency_symbol}}{{website_fee}}) – you may mention it once, calmly, in how_we_help.

TONE: a helpful senior consultant giving honest advice. Respectful, specific, encouraging. No hype, no pressure, no fake numbers.`},
	{"whatsapp", "Default – WhatsApp first message", "", `Hello {{business_name}} team 👋

This is {{sender_name}} from {{agency_name}}. We ran a free audit of your Google Maps listing and website, and found a few things that may be costing you customer enquiries.

I have attached your personalised report (PDF) – please have a look, it takes just 2 minutes.

Shall I explain the key points on a quick call?`},
	{"whatsapp_noweb", "Default – WhatsApp (no website)", "", `Hello {{business_name}} team 👋

This is {{sender_name}} from {{agency_name}}. We checked your Google Maps listing and noticed you don't have a website yet – many customers search on Google before calling.

I have attached a free Google Maps audit report and a short website proposal (PDF). Please have a look.

Shall I explain it on a quick call?`},
	{"email", "Default – Email first message", "Free audit: how {{business_name}} can get more customers from Google", `Hi {{business_name}} team,

I'm {{sender_name}} from {{agency_name}}. We ran a free audit of your website ({{website}}) and your Google Maps listing, and found a few quick wins that can help you get more enquiries from Google.

Your personalised report is attached as a PDF.

If you'd like, I can walk you through it in a short 15-minute call – just reply to this email.

Best regards,
{{sender_name}}
{{agency_name}}
{{agency_phone}} | {{agency_website}}`},
	{"email_noweb", "Default – Email (no website)", "{{business_name}}: free Google Maps audit & website idea", `Hi {{business_name}} team,

I'm {{sender_name}} from {{agency_name}}. While checking businesses on Google Maps for "{{keyword}}", we noticed you don't have a website yet.

I've attached a free Google Maps audit report and a short website proposal.

If you'd like, I can walk you through it in a short 15-minute call – just reply to this email.

Best regards,
{{sender_name}}
{{agency_name}}
{{agency_phone}} | {{agency_website}}`},
}

// sampleJSON: example report content used by "Preview design" (Report::sample, no AI cost).
const sampleJSON = `{"summary": "Your business is well known locally and your Google rating is strong, which is a great foundation. Most new customers, however, now compare two or three options on Google Maps before calling, and several smaller gaps make it easier for them to pick a competitor.\n\nThe good news: the biggest gaps are in your Google profile and in how easy it is to contact you from your website – both can be improved within weeks.", "headline": "Closing the review gap and making it one tap to call could add 10–15 enquiries a month.", "strengths": ["Excellent 4.9★ average rating", "Secure, mobile-friendly website", "Ranking in the top 3 for the main search", "Clear contact details on the website"], "market": {"search_term": "digital marketing agency in Pune", "monthly_searches": 1900, "insight": "Business owners in Pune usually shortlist agencies from Google Maps and then check reviews and past work on the website before calling. Proof of results and quick responses matter more than price at the shortlist stage.", "stats": [{"value": "87%", "label": "of consumers used Google to evaluate local businesses", "source": "BrightLocal, 2023"}, {"value": "76%", "label": "of people who search on a phone for something nearby visit a business within a day", "source": "Google/Ipsos, 2016"}, {"value": "3×", "label": "more likely to be considered when a profile is complete", "source": "Google Business Profile Help"}, {"value": "53%", "label": "of mobile visits are abandoned if a page takes over 3 seconds to load", "source": "Google, 2016"}]}, "journey": {"monthly_searches": 1900, "profile_views": 650, "actions": 95, "customers": 9, "note": "Estimated from typical local-pack click shares for a #1–3 position and industry conversion rates."}, "industry_trends": [{"title": "AI answers are changing search", "detail": "Google now shows AI summaries for many local searches. Businesses with complete profiles, reviews and clear service pages are the ones these summaries mention."}, {"title": "Reviews decide the shortlist", "detail": "Most people only consider businesses with 4+ stars and recent reviews. Review volume and freshness now matter as much as the rating itself."}, {"title": "WhatsApp is the new contact form", "detail": "In India most first enquiries now arrive on WhatsApp. A visible WhatsApp button and quick replies convert more visitors into conversations."}], "competitors_insight": "You lead on rating, but the two competitors just below you are collecting reviews faster and have more photos, which helps them appear for more searches.", "findings": [{"area": "Google Maps", "title": "Profile is missing photos and hours", "found": "Only 3 photos and no opening hours on your Google profile.", "why": "Profiles with photos and hours get more calls and appear in “open now” searches.", "fix": "Add 15–20 recent photos and set regular and holiday hours.", "impact": "High", "effort": "Low"}, {"area": "Reviews", "title": "Review growth has slowed", "found": "120 reviews; competitors average 26 but are adding them faster.", "why": "Fresh reviews are a strong ranking and trust signal.", "fix": "Ask every happy client with a short WhatsApp link after each project.", "impact": "High", "effort": "Low"}, {"area": "Conversion", "title": "No analytics on the website", "found": "Google Analytics is not installed.", "why": "You cannot see which pages or channels bring enquiries.", "fix": "Install GA4 with call and form tracking.", "impact": "Medium", "effort": "Low"}, {"area": "Speed", "title": "Homepage loads slowly on mobile", "found": "The homepage HTML alone took 2.9 seconds.", "why": "Visitors leave slow pages before they enquire.", "fix": "Compress images and defer non-essential scripts.", "impact": "Medium", "effort": "Medium"}, {"area": "Content", "title": "Few dedicated service pages", "found": "Only one service page is linked from the homepage.", "why": "Each service needs its own page to appear for its searches.", "fix": "Create one page per core service with local examples.", "impact": "Medium", "effort": "Medium"}], "opportunity": {"customer_value": 60000, "current_enquiries_month": 12, "potential_enquiries_month": 24, "conversion_rate": 20, "assumptions": "Based on your current position, typical local-pack click shares and a conservative 20% enquiry-to-client rate."}, "outcomes": [{"metric": "Calls from Google Maps", "today": "~12 / month", "target": "20–25 / month", "timeframe": "6 months"}, {"metric": "Google reviews", "today": "120", "target": "170+", "timeframe": "6 months"}, {"metric": "Website enquiries", "today": "Not tracked", "target": "Tracked, +30%", "timeframe": "3 months"}], "plan": [{"phase": "Weeks 1–2", "title": "Fix the basics", "actions": ["Complete Google profile", "Install tracking", "Click-to-call & WhatsApp"], "result": "Every enquiry is measurable"}, {"phase": "Weeks 3–4", "title": "Build trust", "actions": ["Review request system", "Add case studies"], "result": "Steady flow of new reviews"}, {"phase": "Weeks 5–8", "title": "Grow visibility", "actions": ["Service pages", "Local citations"], "result": "Appear for more searches"}, {"phase": "Weeks 9–12", "title": "Optimise", "actions": ["Speed improvements", "Monthly reporting"], "result": "More enquiries per visitor"}], "quick_wins": [{"title": "Add 10 new photos", "how": "Office, team and recent work – takes 15 minutes."}, {"title": "Reply to every review", "how": "A short personal reply shows you care."}, {"title": "Set holiday hours", "how": "Avoid losing “open now” searches."}, {"title": "Ask 5 clients for reviews", "how": "Send your review link on WhatsApp today."}, {"title": "Make your number tappable", "how": "Use a tel: link in the header."}], "how_we_help": [{"service": "Google Business Profile management", "what": "Weekly posts, photos and review replies.", "result": "More profile views and calls"}, {"service": "Local SEO", "what": "Service pages and local citations for Pune.", "result": "Visible for more searches"}, {"service": "Conversion & tracking", "what": "Analytics, call tracking and faster pages.", "result": "More enquiries from the same traffic"}], "sources": [{"title": "Local Consumer Review Survey", "publisher": "BrightLocal", "year": "2023", "url": "https://www.brightlocal.com/research/local-consumer-review-survey/"}, {"title": "Understanding consumers’ local search behavior", "publisher": "Google / Ipsos", "year": "2016", "url": "https://www.thinkwithgoogle.com/"}, {"title": "Find out how mobile page speed impacts consumers", "publisher": "Google", "year": "2016", "url": "https://www.thinkwithgoogle.com/"}, {"title": "Tips to improve your local ranking on Google", "publisher": "Google Business Profile Help", "year": "2024", "url": "https://support.google.com/business/answer/7091"}], "closing": "Thank you for taking the time to read this. Even if you only act on the quick wins, you should see a difference within a few weeks – and we are always happy to answer questions."}`
