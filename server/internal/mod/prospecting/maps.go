package prospecting

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/narendersinghh/setu/server/internal/netx"
	"github.com/narendersinghh/setu/server/internal/settings"
)

// Google bills Places API (New) per REQUEST at the highest SKU of the fields asked for (not per business, not per field):
//   - Text Search with only places.id = "Essentials (IDs only)" → free & unlimited (used to skip businesses you already have)
//   - one Text Search page (up to 20 businesses) with phone/website/rating/hours = 1 "Text Search Enterprise" request
//   - + editorialSummary (business description) = 1 "Text Search Enterprise + Atmosphere" request (dearer) – off by default
//   - Place Details for ONE business with the same fields = 1 "Place Details Enterprise" request
const placeFields = "id,displayName,formattedAddress,addressComponents,nationalPhoneNumber,internationalPhoneNumber,websiteUri,rating,userRatingCount,googleMapsUri,primaryTypeDisplayName,types,regularOpeningHours.weekdayDescriptions,businessStatus,location,photos.name"

// List prices per request (USD) used to pick the cheapest way to fetch new businesses and to record usage cost.
var skuPrice = map[string]float64{
	"google_places":              0.035,
	"google_places_atmo":         0.040,
	"google_places_ids":          0,
	"google_places_details":      0.020,
	"google_places_details_atmo": 0.025,
	"serpapi":                    0.015,
	"pagespeed":                  0,
}

// Place is one normalised business from Google Places or SerpAPI.
type Place struct {
	PlaceID  string   `json:"place_id"`
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Address  string   `json:"address"`
	City     string   `json:"city"`
	Phone    string   `json:"phone"`
	Website  string   `json:"website"`
	Rating   *float64 `json:"rating"`
	Reviews  int      `json:"reviews"`
	MapsURL  string   `json:"maps_url"`
	Lat      *float64 `json:"lat"`
	Lng      *float64 `json:"lng"`
	Raw      PlaceRaw `json:"raw"`
}

type PlaceRaw struct {
	Photos      int      `json:"photos"`
	Hours       []string `json:"hours"`
	Status      string   `json:"status"`
	Description string   `json:"description"`
	Types       []string `json:"types"`
}

// usageFn records one API call (service = billing SKU).
type usageFn func(service, op string, failed bool)

type mapsClient struct {
	s     *settings.Store
	track usageFn
}

func (m *mapsClient) atmo() bool { return m.s.Get("maps_atmosphere", "0") == "1" }

// sku is the usage service key = billing SKU of a full request.
func (m *mapsClient) sku(details bool) string {
	k := "google_places"
	if details {
		k += "_details"
	}
	if m.atmo() {
		k += "_atmo"
	}
	return k
}

func (m *mapsClient) fields(prefix string) string {
	f := placeFields
	if m.atmo() {
		f += ",editorialSummary"
	}
	parts := strings.Split(f, ",")
	for i := range parts {
		parts[i] = prefix + parts[i]
	}
	return strings.Join(parts, ",")
}

func (m *mapsClient) key() (string, error) {
	k := strings.TrimSpace(m.s.Get("google_api_key", ""))
	if k == "" {
		return "", errors.New("Google Places API key missing. Add it in Settings → Map scraper & audits.")
	}
	return k, nil
}

func (m *mapsClient) body(q, next string) map[string]any {
	b := map[string]any{"textQuery": q, "pageSize": 20, "languageCode": m.s.Get("maps_language", "en"), "regionCode": m.s.Get("maps_region", "in")}
	if next != "" {
		b["pageToken"] = next
	}
	return b
}

const placesSearchURL = "https://places.googleapis.com/v1/places:searchText"

var placesBase = "https://places.googleapis.com/v1/places/"

type gPlace struct {
	ID          string `json:"id"`
	DisplayName struct {
		Text string `json:"text"`
	} `json:"displayName"`
	FormattedAddress  string `json:"formattedAddress"`
	AddressComponents []struct {
		LongText string   `json:"longText"`
		Types    []string `json:"types"`
	} `json:"addressComponents"`
	NationalPhoneNumber      string   `json:"nationalPhoneNumber"`
	InternationalPhoneNumber string   `json:"internationalPhoneNumber"`
	WebsiteURI               string   `json:"websiteUri"`
	Rating                   *float64 `json:"rating"`
	UserRatingCount          int      `json:"userRatingCount"`
	GoogleMapsURI            string   `json:"googleMapsUri"`
	PrimaryTypeDisplayName   struct {
		Text string `json:"text"`
	} `json:"primaryTypeDisplayName"`
	Types               []string `json:"types"`
	RegularOpeningHours struct {
		WeekdayDescriptions []string `json:"weekdayDescriptions"`
	} `json:"regularOpeningHours"`
	BusinessStatus string `json:"businessStatus"`
	Location       *struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"location"`
	Photos           []json.RawMessage `json:"photos"`
	EditorialSummary struct {
		Text string `json:"text"`
	} `json:"editorialSummary"`
}

type gSearch struct {
	Places        []gPlace `json:"places"`
	NextPageToken string   `json:"nextPageToken"`
}

func (p gPlace) item() Place {
	city := ""
	for _, c := range p.AddressComponents {
		for _, t := range c.Types {
			if t == "locality" {
				city = c.LongText
				break
			}
		}
		if city != "" {
			break
		}
	}
	cat := p.PrimaryTypeDisplayName.Text
	if cat == "" && len(p.Types) > 0 {
		cat = strings.ReplaceAll(p.Types[0], "_", " ")
	}
	phone := p.InternationalPhoneNumber
	if phone == "" {
		phone = p.NationalPhoneNumber
	}
	it := Place{PlaceID: p.ID, Name: p.DisplayName.Text, Category: cat, Address: p.FormattedAddress, City: city, Phone: phone,
		Website: p.WebsiteURI, Rating: p.Rating, Reviews: p.UserRatingCount, MapsURL: p.GoogleMapsURI,
		Raw: PlaceRaw{Photos: len(p.Photos), Hours: p.RegularOpeningHours.WeekdayDescriptions, Status: p.BusinessStatus, Description: p.EditorialSummary.Text, Types: p.Types}}
	if p.Location != nil {
		la, ln := p.Location.Latitude, p.Location.Longitude
		it.Lat, it.Lng = &la, &ln
	}
	if it.Raw.Hours == nil {
		it.Raw.Hours = []string{}
	}
	if it.Raw.Types == nil {
		it.Raw.Types = []string{}
	}
	return it
}

func (m *mapsClient) post(ctx context.Context, q, next, mask string, timeoutSec int) (*netx.Resp, *gSearch, error) {
	key, err := m.key()
	if err != nil {
		return nil, nil, err
	}
	r, err := apiDo(ctx, netx.Req{URL: placesSearchURL, JSON: m.body(q, next), Headers: map[string]string{"X-Goog-Api-Key": key, "X-Goog-FieldMask": mask}, Timeout: secs(timeoutSec)})
	if err != nil {
		return nil, nil, err
	}
	var out gSearch
	if r.Code == 200 {
		if err := r.JSON(&out); err != nil {
			return r, nil, fmt.Errorf("Google Places: invalid answer: %w", err)
		}
	}
	return r, &out, nil
}

// places: one full result page (1 billable Text Search request).
func (m *mapsClient) places(ctx context.Context, q, next string) ([]Place, string, error) {
	r, out, err := m.post(ctx, q, next, m.fields("places.")+",nextPageToken", 40)
	m.track(m.sku(false), "text_search", err != nil || r.Code != 200)
	if err != nil {
		return nil, "", err
	}
	if r.Code != 200 {
		return nil, "", errors.New(gerr(r))
	}
	items := make([]Place, 0, len(out.Places))
	for _, p := range out.Places {
		items = append(items, p.item())
	}
	return items, out.NextPageToken, nil
}

// ids: free check – only the place IDs of one result page (Text Search Essentials IDs only – no charge).
func (m *mapsClient) ids(ctx context.Context, q, next string) ([]string, string, error) {
	r, out, err := m.post(ctx, q, next, "places.id,nextPageToken", 30)
	m.track("google_places_ids", "text_search_ids", err != nil || r.Code != 200)
	if err != nil {
		return nil, "", err
	}
	if r.Code != 200 {
		return nil, "", errors.New(gerr(r))
	}
	ids := make([]string, 0, len(out.Places))
	for _, p := range out.Places {
		ids = append(ids, p.ID)
	}
	return ids, out.NextPageToken, nil
}

// details: full data for one business (1 Place Details request).
func (m *mapsClient) details(ctx context.Context, id string) (Place, error) {
	key, err := m.key()
	if err != nil {
		return Place{}, err
	}
	u := placesBase + url.PathEscape(id) + "?" + url.Values{"languageCode": {m.s.Get("maps_language", "en")}, "regionCode": {m.s.Get("maps_region", "in")}}.Encode()
	r, err := apiDo(ctx, netx.Req{URL: u, Headers: map[string]string{"X-Goog-Api-Key": key, "X-Goog-FieldMask": m.fields("")}, Timeout: secs(30)})
	m.track(m.sku(true), "place_details", err != nil || r.Code != 200)
	if err != nil {
		return Place{}, err
	}
	if r.Code != 200 {
		return Place{}, errors.New(gerr(r))
	}
	var p gPlace
	if err := r.JSON(&p); err != nil {
		return Place{}, err
	}
	return p.item(), nil
}

var gfix = map[string]string{
	"SERVICE_DISABLED":              `Enable "Places API (New)" in the SAME Google Cloud project as this key.`,
	"API_KEY_SERVICE_BLOCKED":       `The key's API restrictions block Places API (New) – add it (or set "Don't restrict key").`,
	"BILLING_DISABLED":              `Link an active billing account to the project (Billing → Link a billing account).`,
	"API_KEY_IP_ADDRESS_BLOCKED":    `The key is restricted to other IP addresses – set Application restrictions to None or add your server IP.`,
	"API_KEY_HTTP_REFERRER_BLOCKED": `Website (referrer) restrictions do not work for server calls – set Application restrictions to None.`,
	"API_KEY_INVALID":               `The API key is wrong – copy it again with "Show key" and paste it in Settings → Map scraper & audits.`,
	"RATE_LIMIT_EXCEEDED":           `Quota exceeded – check Quotas for Places API (New).`,
}

// gerr: readable Google error with the exact reason code and how to fix it.
func gerr(r *netx.Resp) string {
	var d struct {
		Error struct {
			Message string `json:"message"`
			Details []struct {
				Reason   string `json:"reason"`
				Metadata struct {
					Consumer string `json:"consumer"`
				} `json:"metadata"`
				Links []struct {
					URL string `json:"url"`
				} `json:"links"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(r.Body, &d)
	m := d.Error.Message
	if m == "" {
		m = string(r.Body)
		if len(m) > 300 {
			m = m[:300]
		}
	}
	msg := "Google Places error " + strconv.Itoa(r.Code) + ": " + m
	var why, links []string
	seen := map[string]bool{}
	for _, x := range d.Error.Details {
		if x.Reason != "" {
			w := x.Reason
			if x.Metadata.Consumer != "" {
				w += " (" + x.Metadata.Consumer + ")"
			}
			if !seen[w] {
				seen[w] = true
				why = append(why, w)
			}
		}
		for _, l := range x.Links {
			links = append(links, l.URL)
		}
	}
	if len(why) > 0 {
		msg += " [reason: " + strings.Join(why, ", ") + "]"
	}
	link := ""
	if len(links) > 0 {
		link = " " + links[0]
	}
	for _, w := range why {
		if f, ok := gfix[strings.Fields(w)[0]]; ok {
			return msg + " – Fix: " + f + link
		}
	}
	if r.Code == 403 {
		msg += " – Most common cause when the key restrictions look correct: billing is not enabled or the billing account is closed/unverified for this project (Google Cloud → Billing). Also confirm the key and the enabled API are in the same project, and wait 5 minutes after changes." + link
	}
	return msg
}

// diagnose: step-by-step check used by Settings → Map scraper & audits → Test.
func (m *mapsClient) diagnose(ctx context.Context, city string) []string {
	key := m.s.Get("google_api_key", "")
	if key == "" {
		return []string{"✕ No Google API key saved in Settings → Map scraper & audits."}
	}
	sp := ""
	if strings.ContainsAny(key, " \t\r\n") {
		sp = ", CONTAINS SPACES – re-paste it"
	}
	head, tail := key, key
	if len(key) > 6 {
		head = key[:6]
	}
	if len(key) > 4 {
		tail = key[len(key)-4:]
	}
	out := []string{fmt.Sprintf("Key in use: %s…%s (%d chars%s)", head, tail, len(key), sp)}
	for _, t := range [][3]string{
		{"Basic search (IDs only – free)", "places.id", "google_places_ids"},
		{"Full search (name, phone, website, rating… – 1 billable request)", "places.id,places.displayName,places.nationalPhoneNumber,places.websiteUri,places.rating", "google_places"},
	} {
		r, err := apiDo(ctx, netx.Req{URL: placesSearchURL, JSON: map[string]any{"textQuery": "cafe in " + city, "pageSize": 3}, Headers: map[string]string{"X-Goog-Api-Key": key, "X-Goog-FieldMask": t[1]}, Timeout: secs(30)})
		ok := err == nil && r.Code == 200
		m.track(t[2], "test", !ok)
		if err != nil {
			out = append(out, "✕ "+t[0]+": "+err.Error())
			break
		}
		if !ok {
			out = append(out, "✕ "+t[0]+": "+gerr(r))
			break
		}
		var res gSearch
		_ = r.JSON(&res)
		out = append(out, fmt.Sprintf("✓ %s: %d results", t[0], len(res.Places)))
	}
	return out
}

// ---------------------------------------------------------------- SerpAPI (Google Maps engine, 100+ results)

var serpURL = "https://serpapi.com/search.json"

var zipTail = regexp.MustCompile(`\s*\d{5,6}$`)

func (m *mapsClient) serp(ctx context.Context, q, next string) ([]Place, string, error) {
	key := strings.TrimSpace(m.s.Get("serpapi_key", ""))
	if key == "" {
		return nil, "", errors.New("SerpAPI key missing. Add it in Settings → Map scraper & audits.")
	}
	start, _ := strconv.Atoi(next)
	u := serpURL + "?" + url.Values{"engine": {"google_maps"}, "type": {"search"}, "q": {q}, "hl": {m.s.Get("maps_language", "en")}, "gl": {m.s.Get("maps_region", "in")}, "start": {strconv.Itoa(start)}, "api_key": {key}}.Encode()
	r, err := apiDo(ctx, netx.Req{URL: u, Timeout: secs(60)})
	var d struct {
		Error        string `json:"error"`
		LocalResults []struct {
			PlaceID           string   `json:"place_id"`
			Title             string   `json:"title"`
			Type              string   `json:"type"`
			Types             []string `json:"types"`
			Address           string   `json:"address"`
			Phone             string   `json:"phone"`
			Website           string   `json:"website"`
			Rating            *float64 `json:"rating"`
			Reviews           int      `json:"reviews"`
			PhotosCount       int      `json:"photos_count"`
			OperatingHours    any      `json:"operating_hours"`
			PermanentlyClosed bool     `json:"permanently_closed"`
			Description       string   `json:"description"`
			GPS               *struct {
				Latitude  float64 `json:"latitude"`
				Longitude float64 `json:"longitude"`
			} `json:"gps_coordinates"`
		} `json:"local_results"`
	}
	if err == nil {
		_ = r.JSON(&d)
	}
	m.track("serpapi", "maps_search", err != nil || r.Code != 200)
	if err != nil {
		return nil, "", errors.New("SerpAPI error: " + err.Error())
	}
	if r.Code != 200 || d.Error != "" {
		if d.Error != "" && strings.Contains(strings.ToLower(d.Error), "hasn't returned any results") {
			return nil, "", nil
		}
		e := d.Error
		if e == "" {
			e = strconv.Itoa(r.Code)
		}
		return nil, "", errors.New("SerpAPI error: " + e)
	}
	out := make([]Place, 0, len(d.LocalResults))
	for _, p := range d.LocalResults {
		parts := strings.Split(p.Address, ",")
		city := ""
		if len(parts) >= 2 {
			city = zipTail.ReplaceAllString(strings.TrimSpace(parts[len(parts)-2]), "")
		}
		id := p.PlaceID
		maps := ""
		if id == "" {
			h := md5.Sum([]byte(p.Title + p.Address))
			id = hex.EncodeToString(h[:])
		} else {
			maps = "https://www.google.com/maps/place/?q=place_id:" + p.PlaceID
		}
		st := "OPERATIONAL"
		if p.PermanentlyClosed {
			st = "CLOSED_PERMANENTLY"
		}
		it := Place{PlaceID: id, Name: p.Title, Category: p.Type, Address: p.Address, City: city, Phone: p.Phone, Website: p.Website, Rating: p.Rating, Reviews: p.Reviews, MapsURL: maps,
			Raw: PlaceRaw{Photos: p.PhotosCount, Hours: serpHours(p.OperatingHours), Status: st, Description: p.Description, Types: p.Types}}
		if it.Raw.Types == nil {
			it.Raw.Types = []string{}
		}
		if p.GPS != nil {
			la, ln := p.GPS.Latitude, p.GPS.Longitude
			it.Lat, it.Lng = &la, &ln
		}
		out = append(out, it)
	}
	nx := ""
	if len(out) >= 20 && start < 100 {
		nx = strconv.Itoa(start + 20)
	}
	return out, nx, nil
}

// serpHours turns SerpAPI operating hours ({"monday":"9 am–5 pm",…}) into weekday lines.
func serpHours(v any) []string {
	out := []string{}
	switch h := v.(type) {
	case map[string]any:
		for _, d := range []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"} {
			if x, ok := h[d]; ok {
				out = append(out, strings.ToUpper(d[:1])+d[1:]+": "+fmt.Sprint(x))
			}
		}
	case []any:
		for _, x := range h {
			out = append(out, fmt.Sprint(x))
		}
	}
	return out
}
