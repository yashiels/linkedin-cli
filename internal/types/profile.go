package types

// Profile is a LinkedIn member profile summary.
type Profile struct {
	URN        string `json:"urn"`
	VanityName string `json:"vanityName"`
	FirstName  string `json:"firstName"`
	LastName   string `json:"lastName"`
	// Headline is the member's current professional headline.
	Headline string `json:"headline"`
	// Location is the displayed location string (city, country).
	Location string `json:"location"`
	// About is the "About" / summary section text.
	About string `json:"about,omitempty"`
	// Connections is the display-formatted connection count, e.g. "500+".
	Connections string `json:"connections,omitempty"`

	Experience []Experience `json:"experience"`
	Education  []Education  `json:"education"`
	Skills     []string     `json:"skills,omitempty"`
}

// FullName returns the member's formatted full name.
func (p *Profile) FullName() string {
	if p.LastName == "" {
		return p.FirstName
	}
	return p.FirstName + " " + p.LastName
}

// Experience is a single work position in a member's profile.
type Experience struct {
	Title      string `json:"title"`
	Company    string `json:"company"`
	CompanyURN string `json:"companyUrn,omitempty"`
	StartDate  string `json:"startDate"` // "2023" or "Jan 2023"
	EndDate    string `json:"endDate"`   // "Present" or "2024"
	Location   string `json:"location,omitempty"`
	// Description is the free-text position description (may be empty).
	Description string `json:"description,omitempty"`
}

// DisplayDates returns the formatted "StartDate - EndDate" range.
func (e *Experience) DisplayDates() string {
	if e.StartDate == "" && e.EndDate == "" {
		return ""
	}
	if e.EndDate == "" || e.EndDate == "Present" {
		if e.StartDate == "" {
			return "Present"
		}
		return e.StartDate + " - Present"
	}
	return e.StartDate + " - " + e.EndDate
}

// Education is a single education entry in a member's profile.
type Education struct {
	School       string `json:"school"`
	Degree       string `json:"degree"`
	FieldOfStudy string `json:"fieldOfStudy"`
	StartDate    string `json:"startDate"` // typically just "2018"
	EndDate      string `json:"endDate"`   // typically just "2021"
}

// DisplayDegree returns a short human-readable "Degree, Field" string.
func (e *Education) DisplayDegree() string {
	switch {
	case e.Degree != "" && e.FieldOfStudy != "":
		return e.Degree + " " + e.FieldOfStudy
	case e.Degree != "":
		return e.Degree
	default:
		return e.FieldOfStudy
	}
}

// DisplayDates returns the formatted year range.
func (e *Education) DisplayDates() string {
	if e.StartDate == "" && e.EndDate == "" {
		return ""
	}
	if e.EndDate == "" {
		return e.StartDate
	}
	return e.StartDate + " - " + e.EndDate
}
