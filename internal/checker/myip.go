package checker

type myIP struct {
	CC          string `json:"cc"`
	CountryCode string `json:"countryCode"` // ip-api.com
	Country     string `json:"country"`
	IP          string `json:"ip"`
	Query       string `json:"query"` // ip-api.com uses "query" for IP
}

// normalize copies alternate JSON fields into the primary fields so that
// consumers always read from .CC, .IP, .Country regardless of which API
// format was parsed.
func (m *myIP) normalize() {
	if m.IP == "" && m.Query != "" {
		m.IP = m.Query
	}
	if m.CC == "" && m.CountryCode != "" {
		m.CC = m.CountryCode
	}
}
