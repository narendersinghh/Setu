package automation

// Definitions shown in the editor (same labels and options as v1 Automation.php).

type Trigger struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Desc  string `json:"desc"`
	Value string `json:"value"` // label of the trigger value ("" = none)
}

var Triggers = []Trigger{
	{"lead_created", "New lead added", "Any new lead in Leads (form, ads, webhook, manual, import or moved from scraped data).", "Source (optional)"},
	{"status_changed", "Status changes to", "When a lead moves to a status.", "Status"},
	{"tag_added", "Tag added", "When a tag is added to a lead.", "Tag"},
	{"replied", "Lead replied", "When a reply is detected (email, WhatsApp) or logged.", ""},
	{"report_ready", "Audit report ready", "When the PDF audit report is generated.", ""},
	{"appointment_booked", "Appointment booked", "When a meeting is booked (booking page or manually).", ""},
	{"call_completed", "AI call finished", "When an AI voice call ends.", ""},
	{"no_reply", "No reply after", "Contacted but no reply after N days.", "Days"},
	{"follow_up_due", "Follow-up date reached", "When the lead's follow-up date/time arrives.", ""},
}

func triggerOK(k string) bool {
	for _, t := range Triggers {
		if t.Key == k {
			return true
		}
	}
	return false
}

func triggerLabel(k string) string {
	for _, t := range Triggers {
		if t.Key == k {
			return t.Label
		}
	}
	return k
}

// eventTriggers are fired by domain events; the others are periodic scans.
var eventTriggers = []string{"lead_created", "status_changed", "tag_added", "replied", "report_ready", "appointment_booked", "call_completed"}

type Action struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Ic    string `json:"ic"`
	C     string `json:"c"`
}

var Actions = []Action{
	{"whatsapp", "Send WhatsApp", "wa", "green"},
	{"email", "Send email", "mail", "blue"},
	{"call", "AI voice call", "mic", "purple"},
	{"booking", "Send booking link", "cal", "orange"},
	{"wait", "Wait", "clock", "gray"},
	{"status", "Change status", "target", "teal"},
	{"tag", "Add tag", "tag", "gray"},
	{"assign", "Assign to user", "user", "gray"},
	{"followup", "Set follow-up", "clock", "gray"},
	{"notify", "Notify team (email)", "alert", "red"},
	{"report", "Run audit & report", "file", "gold"},
	{"webhook", "Send to webhook", "plug", "teal"},
}

func actionOK(k string) bool {
	for _, x := range Actions {
		if x.Key == k {
			return true
		}
	}
	return false
}

// Conds: lead fields usable in conditions (custom fields are added as custom.<key>).
var Conds = [][2]string{{"source", "Source"}, {"status", "Status"}, {"city", "City"}, {"service", "Service"}, {"tags", "Tags"}, {"email", "Email"}, {"phone", "Phone"}, {"website", "Website"}, {"category", "Category"}}

var Ops = [][2]string{{"is", "is"}, {"not", "is not"}, {"contains", "contains"}, {"empty", "is empty"}, {"not_empty", "is not empty"}}

const Vars = "{{first_name}} {{contact_name}} {{business_name}} {{phone}} {{email}} {{city}} {{service}} {{booking_link}} {{report_link}} {{agency_name}} {{sender_name}}"

// Recipe is a ready-made automation (v1 Automation::templates).
type Recipe struct {
	Key   string           `json:"key"`
	Name  string           `json:"name"`
	Desc  string           `json:"desc"`
	Ic    string           `json:"ic"`
	C     string           `json:"c"`
	Trig  string           `json:"trig"`
	TV    string           `json:"trig_value"`
	Conds []Cond           `json:"conditions"`
	Steps []map[string]any `json:"steps"`
}

var Recipes = []Recipe{
	{Key: "welcome", Name: "Instant WhatsApp welcome", Desc: "Reply to every new enquiry within seconds and alert your team.", Ic: "wa", C: "green", Trig: "lead_created", Steps: []map[string]any{
		{"t": "whatsapp", "text": "Hi {{first_name}} 👋 Thanks for contacting {{agency_name}}! We have received your enquiry and will call you shortly.\n\nPrefer a time that suits you? Book here: {{booking_link}}"},
		{"t": "notify", "subject": "New lead: {{contact_name}} ({{business_name}})", "text": "New lead: {{contact_name}} – {{phone}} – {{email}}"},
		{"t": "status", "v": "contacted"}}},
	{Key: "aicall", Name: "Speed-to-lead AI call", Desc: "An AI voice agent calls new ad leads within 2 minutes to qualify them.", Ic: "mic", C: "violet", Trig: "lead_created", TV: "facebook", Steps: []map[string]any{
		{"t": "wait", "n": 2, "u": "minutes"},
		{"t": "call", "task": "", "first": ""},
		{"t": "tag", "v": "ai-called"}}},
	{Key: "noreply", Name: "No-reply follow-up sequence", Desc: "Contacted but silent for 3 days? Nudge on WhatsApp, then email, then an AI call.", Ic: "clock", C: "orange", Trig: "no_reply", TV: "3", Steps: []map[string]any{
		{"t": "whatsapp", "text": "Hi {{first_name}}, just checking if you had a chance to look at the report we shared? Happy to explain it in a quick 15-minute call."},
		{"t": "wait", "n": 2, "u": "days"},
		{"t": "email", "subject": "Quick follow-up – {{business_name}}", "body": "Hi {{first_name}},\n\nI wanted to follow up on the audit we sent. You can pick a time for a free call here: {{booking_link}}\n\nRegards,\n{{sender_name}}", "attach": 0},
		{"t": "wait", "n": 3, "u": "days"},
		{"t": "call", "task": "", "first": ""},
		{"t": "tag", "v": "follow-up-done"}}},
	{Key: "book", Name: "Qualified → book a meeting", Desc: "When a lead becomes Qualified, send the booking link and remind once.", Ic: "cal", C: "teal", Trig: "status_changed", TV: "qualified", Steps: []map[string]any{
		{"t": "booking", "ch": "whatsapp", "text": "Hi {{first_name}}, great speaking with you! Please pick a convenient time for our strategy call:"},
		{"t": "wait", "n": 1, "u": "days"},
		{"t": "booking", "ch": "email", "subject": "Pick a time for your strategy call", "text": "Hi {{first_name}},\n\nHere is the link to choose a time that suits you:"}}},
	{Key: "booked", Name: "Meeting booked → confirm & prepare", Desc: "Confirm on WhatsApp, notify the team and run an audit before the meeting.", Ic: "check", C: "blue", Trig: "appointment_booked", Steps: []map[string]any{
		{"t": "whatsapp", "text": "Thanks {{first_name}}! Your meeting with {{agency_name}} is confirmed. We'll share the meeting link before the call."},
		{"t": "notify", "subject": "Meeting booked: {{contact_name}}", "text": "Meeting booked by {{contact_name}} ({{business_name}}) – {{phone}}"},
		{"t": "report", "contact": 0}}},
	{Key: "report", Name: "Report sent → follow up", Desc: "One day after the audit report, check in on WhatsApp; call if still silent.", Ic: "file", C: "amber", Trig: "report_ready", Steps: []map[string]any{
		{"t": "wait", "n": 1, "u": "days"},
		{"t": "whatsapp", "text": "Hi {{business_name}} team, did you get a chance to see the audit report? The quick wins on page 2 can bring more calls this month."},
		{"t": "wait", "n": 2, "u": "days"},
		{"t": "call", "task": "", "first": ""}}},
}

func recipe(k string) *Recipe {
	for i := range Recipes {
		if Recipes[i].Key == k {
			return &Recipes[i]
		}
	}
	return nil
}
