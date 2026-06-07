package types

// JobCard is a lightweight representation of a job from search results.
type JobCard struct {
	URN            string `json:"urn"`
	ID             string `json:"id"`
	Title          string `json:"title"`
	Company        string `json:"company"`
	CompanyURN     string `json:"companyUrn,omitempty"`
	Location       string `json:"location"`
	PostedAt       string `json:"postedAt"`
	ApplicantCount string `json:"applicantCount,omitempty"`
	Remote         bool   `json:"remote"`
	EasyApply      bool   `json:"easyApply"`
	Saved          bool   `json:"saved"`
	ListingURL     string `json:"listingUrl,omitempty"`
}

// JobDetail is the full representation of a job posting.
type JobDetail struct {
	JobCard

	Description    string   `json:"description"`
	Salary         string   `json:"salary,omitempty"`
	SalaryMin      int64    `json:"salaryMin,omitempty"`
	SalaryMax      int64    `json:"salaryMax,omitempty"`
	SalaryCurr     string   `json:"salaryCurrency,omitempty"`
	SeniorityLevel string   `json:"seniorityLevel,omitempty"`
	EmploymentType string   `json:"employmentType,omitempty"`
	Industries     []string `json:"industries,omitempty"`
	Skills         []string `json:"skills,omitempty"`
	HiringManager  string   `json:"hiringManager,omitempty"`
	ClosedAt       string   `json:"closedAt,omitempty"`
	Expired        bool     `json:"expired"`
}
